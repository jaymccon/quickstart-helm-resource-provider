package resource

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/gofrs/flock"
	"github.com/pkg/errors"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/downloader"
	"helm.sh/helm/v3/pkg/getter"
	"helm.sh/helm/v3/pkg/kube"
	"helm.sh/helm/v3/pkg/repo"
	"sigs.k8s.io/yaml"
)

const (
	helmCacheHomeEnvVar  = "/tmp/cache"
	helmConfigHomeEnvVar = "/tmp/config"
	helmDataHomeEnvVar   = "/tmp/data"
	stableRepoURL        = "https://kubernetes-charts.storage.googleapis.com"
	chartLocalPath       = "/tmp/chart.tgz"
)

type helmStatusData struct {
	status       string
	namespace    string
	chartName    string
	chartVersion string
	chart        string
	manifest     string
}
type helmListData struct {
	releaseName  string
	chartName    string
	chartVersion string
	chart        string
}

// helmClientInvoke generates the namespaced helm client
func helmClientInvoke(namespace *string) (*action.Configuration, error) {
	if namespace == nil {
		namespace = aws.String("default")
	}
	actionConfig := new(action.Configuration)
	if err := actionConfig.Init(kube.GetConfig(kubeConfigLocalPath, "", *namespace), *namespace, os.Getenv("HELM_DRIVER"), func(format string, v ...interface{}) {
		fmt.Sprintf(format, v)
	}); err != nil {
		return nil, genericError("Helm client", err)
	}
	return actionConfig, nil
}

// addHelmRepoUpdate Add the repo and fire repo update
func addHelmRepoUpdate(name string, url string, settings *cli.EnvSettings) error {
	file := settings.RepositoryConfig
	os.Remove(file)
	//Ensure the file directory exists as it is required for file locking
	err := os.MkdirAll(filepath.Dir(file), os.ModePerm)
	if err != nil && !os.IsExist(err) {
		return genericError("Adding helm repository", err)
	}

	// Acquire a file lock for process synchronization
	fileLock := flock.New(strings.Replace(file, filepath.Ext(file), ".lock", 1))
	lockCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	locked, err := fileLock.TryLockContext(lockCtx, time.Second)
	if err == nil && locked {
		defer fileLock.Unlock()
	}
	if err != nil {
		return genericError("Adding helm repository", err)
	}

	b, err := ioutil.ReadFile(file)
	if err != nil && !os.IsNotExist(err) {
		return genericError("Adding helm repository", err)
	}

	var f repo.File
	if err := yaml.Unmarshal(b, &f); err != nil {
		return genericError("Adding helm repository", err)
	}

	c := repo.Entry{
		Name: name,
		URL:  url,
	}
	r, err := repo.NewChartRepository(&c, getter.All(settings))
	if err != nil {
		return genericError("Adding helm repository", err)
	}

	if _, err := r.DownloadIndexFile(); err != nil {
		return genericError("Adding helm repository", errors.Wrapf(err, "looks like %q is not a valid chart repository or cannot be reached", url))
	}

	f.Update(&c)

	if err := f.WriteFile(file, 0644); err != nil {
		return genericError("Adding helm repository", err)
	}
	log.Printf("%q has been added to your repositories\n", name)
	var repos []*repo.ChartRepository
	for _, cfg := range f.Repositories {
		r, err := repo.NewChartRepository(cfg, getter.All(settings))
		if err != nil {
			genericError("Adding helm repository", err)
		}
		repos = append(repos, r)
	}
	log.Printf("Hang tight while we grab the latest from your chart repositories...")
	var wg sync.WaitGroup
	for _, re := range repos {
		wg.Add(1)
		go func(re *repo.ChartRepository) {
			defer wg.Done()
			if _, err := re.DownloadIndexFile(); err != nil {
				log.Printf("...Unable to get an update from the %q chart repository (%s):\n\t%s\n", re.Config.Name, re.Config.URL, err)
			} else {
				log.Printf("...Successfully got an update from the %q chart repository\n", re.Config.Name)
			}
		}(re)
	}
	wg.Wait()
	log.Printf("Update Complete. ⎈ Happy Helming!⎈ ")
	return nil
}

// helmInstall invokes the helm uninstall client
func (c *Client) helmInstall(config *Config, values map[string]interface{}) error {
	log.Printf("Installing release %s", *config.name)
	var cp string
	var err error
	client := action.NewInstall(c.helmClient)
	client.ReleaseName = *config.name
	if config.version != nil {
		client.Version = *config.version
	}
	switch *config.repoType {
	case "Remote":
		err = addHelmRepoUpdate(*config.repoName, *config.repoURL, c.settings)
		if err != nil {
			return genericError("Helm Upgrade", err)
		}
		cp, err = client.ChartPathOptions.LocateChart(*config.chart, c.settings)
		if err != nil {
			return genericError("Helm Upgrade", err)
		}
	default:
		err = c.downloadChart(*config.chartPath, chartLocalPath)
		if err != nil {
			return err
		}
		cp = *config.chart
	}
	p := getter.All(c.settings)
	chartRequested, err := loader.Load(cp)
	if err != nil {
		return genericError("Helm install", err)
	}

	if req := chartRequested.Metadata.Dependencies; req != nil {
		if err := action.CheckDependencies(chartRequested, req); err != nil {
			if client.DependencyUpdate {
				man := &downloader.Manager{
					ChartPath:        cp,
					Keyring:          client.ChartPathOptions.Keyring,
					SkipUpdate:       false,
					Getters:          p,
					RepositoryConfig: c.settings.RepositoryConfig,
					RepositoryCache:  c.settings.RepositoryCache,
				}
				if err := man.Update(); err != nil {
					return genericError("Helm install", err)
				}
			} else {
				return genericError("Helm install", err)
			}
		}
	}

	c.createNamespace(*config.namespace)
	if err != nil {
		return err
	}
	client.Namespace = *config.namespace

	rel, err := client.Run(chartRequested, values)
	if err != nil {
		return genericError("Helm install", err)
	}
	fmt.Println("Successfully installed release: ", rel.Name)
	return nil
}

// helmUninstall invokes the helm uninstaller client
func (c *Client) helmUninstall(name string) error {
	log.Printf("Uninstalling release %s", name)
	client := action.NewUninstall(c.helmClient)
	res, err := client.Run(name)
	if err != nil {
		return genericError("Helm Uninstall", err)
	}
	if res != nil && res.Info != "" {
		log.Printf(res.Info)
	}
	log.Printf("Release \"%s\" uninstalled\n", name)
	return nil
}

// helmStatus check the status for specified release
func (c *Client) helmStatus(name string) (*helmStatusData, error) {
	log.Printf("Checking release status %s", name)
	h := &helmStatusData{}
	client := action.NewStatus(c.helmClient)
	res, err := client.Run(name)
	if err != nil {
		return nil, err
	}
	if res != nil {
		h.namespace = res.Namespace
		h.manifest = res.Manifest
		if res.Info != nil {
			h.status = res.Info.Status.String()
		}
		if res.Chart != nil {
			h.chartName = res.Chart.Metadata.Name
			h.chartVersion = res.Chart.Metadata.Version
			h.chart = res.Chart.Metadata.Name + "-" + res.Chart.Metadata.Version
		}
	}
	log.Printf("Found release in %s status", h.status)
	return h, nil
}

// helmList list the release with specific chart and version in a namespace.
func (c *Client) helmList(config *Config) (*helmListData, error) {
	l := &helmListData{}
	client := action.NewList(c.helmClient)
	client.All = true
	client.AllNamespaces = true
	client.SetStateMask()
	res, err := client.Run()
	if err != nil {
		return nil, err
	}
	for _, r := range res {
		if config.version != nil {
			if r.Namespace == *config.namespace && r.Chart.Metadata.Name == *config.chartName && r.Chart.Metadata.Version == *config.version {
				l.releaseName = r.Name
			}
		} else {
			if r.Namespace == *config.namespace && r.Chart.Metadata.Name == *config.chartName {
				l.releaseName = r.Name
			}
			l.chartName = r.Chart.Metadata.Name
			l.chartVersion = r.Chart.Metadata.Version
			l.chartVersion = r.Chart.Metadata.Name + "-" + r.Chart.Metadata.Version
		}
	}
	return l, nil
}

// helmUpgrade invokes the helm upgrade client
func (c *Client) helmUpgrade(name string, config *Config, values map[string]interface{}) error {
	log.Printf("Upgrading release %s", name)
	client := action.NewUpgrade(c.helmClient)
	var cp string
	var err error
	if config.version != nil {
		client.Version = *config.version
	}
	switch *config.repoType {
	case "Remote":
		err = addHelmRepoUpdate(*config.repoName, *config.repoURL, c.settings)
		if err != nil {
			return genericError("Helm Upgrade", err)
		}
		cp, err = client.ChartPathOptions.LocateChart(*config.chart, c.settings)
		if err != nil {
			return genericError("Helm Upgrade", err)
		}
	default:
		err = c.downloadChart(*config.chartName, chartLocalPath)
		if err != nil {
			return err
		}
		cp = *config.chart
	}
	// Check chart dependencies to make sure all are present in /charts
	ch, err := loader.Load(cp)
	if err != nil {
		return genericError("Helm Upgrade", err)
	}
	if req := ch.Metadata.Dependencies; req != nil {
		if err := action.CheckDependencies(ch, req); err != nil {
			return genericError("Helm Upgrade", err)
		}
	}

	rel, err := client.Run(name, ch, values)
	if err != nil {
		return genericError("Helm Upgrade", err)
	}
	log.Printf("Release %q has been upgraded. Happy Helming!\n", rel.Name)
	return nil

}
