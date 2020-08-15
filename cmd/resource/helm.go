package resource

import (
	"context"
	"fmt"
	"io/ioutil"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"log"
	"os"
	"path/filepath"
	"regexp"
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
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/repo"
	"sigs.k8s.io/yaml"
)

const (
	HelmCacheHomeEnvVar  = "/tmp/cache"
	HelmConfigHomeEnvVar = "/tmp/config"
	HelmDataHomeEnvVar   = "/tmp/data"
	HelmDriver           = "secret"
	stableRepoURL        = "https://kubernetes-charts.storage.googleapis.com"
	chartLocalPath       = "/tmp/chart.tgz"
)

type HelmStatusData struct {
	Status       release.Status `json:",omitempty"`
	Namespace    string         `json:",omitempty"`
	ChartName    string         `json:",omitempty"`
	ChartVersion string         `json:",omitempty"`
	Chart        string         `json:",omitempty"`
	Manifest     string         `json:",omitempty"`
}
type HelmListData struct {
	ReleaseName  string `json:",omitempty"`
	ChartName    string `json:",omitempty"`
	ChartVersion string `json:",omitempty"`
	Chart        string `json:",omitempty"`
	Namespace    string `json:",omitempty"`
}

// HelmClientInvoke generates the namespaced helm client
func helmClientInvoke(namespace *string, getter genericclioptions.RESTClientGetter) (*action.Configuration, error) {
	if namespace == nil {
		namespace = aws.String("default")
	}
	actionConfig := new(action.Configuration)
	if err := actionConfig.Init(getter, *namespace, os.Getenv("HELM_DRIVER"), func(format string, v ...interface{}) {
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

// HelmInstall invokes the helm uninstall client
func (c *Clients) HelmInstall(config *Config, values map[string]interface{}, chart *Chart) error {
	log.Printf("Installing release %s", *config.Name)
	var cp string
	var err error
	client := action.NewInstall(c.HelmClient)
	client.ReleaseName = *config.Name

	switch *chart.ChartType {
	case "Remote":
		if chart.ChartVersion != nil {
			client.Version = *chart.ChartVersion
		}
		err = addHelmRepoUpdate(*chart.ChartRepo, *chart.ChartRepoURL, c.Settings)
		if err != nil {
			return genericError("Helm Upgrade", err)
		}
		cp, err = client.ChartPathOptions.LocateChart(*chart.Chart, c.Settings)
		if err != nil {
			return genericError("Helm Upgrade", err)
		}
	default:
		err = c.downloadChart(*chart.ChartPath, chartLocalPath)
		if err != nil {
			return err
		}
		cp = *chart.Chart
	}
	p := getter.All(c.Settings)
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
					RepositoryConfig: c.Settings.RepositoryConfig,
					RepositoryCache:  c.Settings.RepositoryCache,
				}
				if err := man.Update(); err != nil {
					return genericError("Helm install", err)
				}
			} else {
				return genericError("Helm install", err)
			}
		}
	}

	err = c.createNamespace(*config.Namespace)
	if err != nil {
		return err
	}
	client.Namespace = *config.Namespace

	rel, err := client.Run(chartRequested, values)
	if err != nil {
		return genericError("Helm install", err)
	}
	log.Println("Successfully installed release: ", rel.Name)
	return nil
}

// HelmUninstall invokes the helm uninstaller client
func (c *Clients) HelmUninstall(name string) error {
	log.Printf("Uninstalling release %s", name)
	client := action.NewUninstall(c.HelmClient)
	res, err := client.Run(name)
	re := regexp.MustCompile(`not found`)
	if err != nil {
		if re.MatchString(err.Error()) {
			log.Printf("Release not found..")
			return nil
		}
		return genericError("Helm Uninstall", err)
	}
	if res != nil && res.Info != "" {
		log.Printf(res.Info)
	}
	log.Printf("Release \"%s\" uninstalled\n", name)
	return nil
}

// HelmStatus check the Status for specified release
func (c *Clients) HelmStatus(name string) (*HelmStatusData, error) {
	log.Printf("Checking release status %s", name)
	h := &HelmStatusData{}
	client := action.NewStatus(c.HelmClient)
	res, err := client.Run(name)
	if err != nil {
		return nil, err
	}
	if res != nil {
		h.Namespace = res.Namespace
		h.Manifest = res.Manifest
		if res.Info != nil {
			h.Status = res.Info.Status
		}
		if res.Chart != nil {
			h.ChartName = res.Chart.Metadata.Name
			h.ChartVersion = res.Chart.Metadata.Version
			h.Chart = res.Chart.Metadata.Name + "-" + res.Chart.Metadata.Version
		}
	}
	log.Printf("Found release in %s status", h.Status)
	return h, nil
}

// HelmList list the release with specific chart and version in a namespace.
func (c *Clients) HelmList(config *Config, chart *Chart) ([]HelmListData, error) {
	a := []HelmListData{}
	l := HelmListData{}
	client := action.NewList(c.HelmClient)
	client.All = true
	client.AllNamespaces = true
	client.SetStateMask()
	res, err := client.Run()
	if err != nil {
		return nil, err
	}
	for _, r := range res {
		if chart.ChartVersion != nil {
			if r.Namespace == *config.Namespace && r.Chart.Metadata.Name == *chart.ChartName && r.Chart.Metadata.Version == *chart.ChartVersion {
				l.ReleaseName = r.Name
				l.Namespace = r.Namespace
				l.ChartName = r.Chart.Metadata.Name
				l.ChartVersion = r.Chart.Metadata.Version
				l.Chart = r.Chart.Metadata.Name + "-" + r.Chart.Metadata.Version
			}
		} else {
			if r.Namespace == *config.Namespace && r.Chart.Metadata.Name == *chart.ChartName {
				l.ReleaseName = r.Name
				l.Namespace = r.Namespace
				l.ChartName = r.Chart.Metadata.Name
				l.ChartVersion = r.Chart.Metadata.Version
				l.Chart = r.Chart.Metadata.Name + "-" + r.Chart.Metadata.Version
			}
		}

		if l.ReleaseName != "" {
			a = append(a, l)
		}
	}
	return a, nil
}

// HelmUpgrade invokes the helm upgrade client
func (c *Clients) HelmUpgrade(name string, config *Config, values map[string]interface{}, chart *Chart) error {
	log.Printf("Upgrading release %s", name)
	client := action.NewUpgrade(c.HelmClient)
	var cp string
	var err error

	switch *chart.ChartType {
	case "Remote":
		if chart.ChartVersion != nil {
			client.Version = *chart.ChartVersion
		}
		err = addHelmRepoUpdate(*chart.ChartRepo, *chart.ChartRepoURL, c.Settings)
		if err != nil {
			return genericError("Helm Upgrade", err)
		}
		cp, err = client.ChartPathOptions.LocateChart(*chart.Chart, c.Settings)
		if err != nil {
			return genericError("Helm Upgrade", err)
		}
	default:
		err = c.downloadChart(*chart.ChartPath, chartLocalPath)
		if err != nil {
			return err
		}
		cp = *chart.Chart
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
