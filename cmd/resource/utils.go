package resource

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/strvals"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/yaml"
)

const (
	valuesYamlFile = "/tmp/values.yaml"
)

// ID struct for CFN physical resource
type ID struct {
	ClusterID  string
	KubeConfig string
	Region     string
	Name       string
	Namespace  string
}

// Client for helm, kube, aws and helm settings
type Client struct {
	helmClient *action.Configuration
	clientSet  *kubernetes.Clientset
	session    *session.Session
	settings   *cli.EnvSettings
	restConfig *rest.Config
}

// Config for processed inputs
type Config struct {
	chart, chartName, chartPath, name, namespace, repoName, repoType, repoURL, version *string
}

//Inputs for Config and Values for helm
type Inputs struct {
	config    *Config
	valueOpts map[string]interface{}
}

// NewClient is for generate clients for helm and kube
func NewClient(cluster *string, kubeconfig *string, namespace *string, ses *session.Session) (*Client, error) {
	c := &Client{}
	var err error
	if err := createKubeConfig(ses, cluster, kubeconfig); err != nil {
		return nil, err
	}
	c.clientSet, c.restConfig, err = kubeClient()
	if err != nil {
		return nil, err
	}
	c.helmClient, err = helmClientInvoke(namespace)
	if err != nil {
		return nil, err
	}
	c.settings = cli.New()
	return c, nil
}

//Process the inputs to the requirements
func (c *Client) processInputs(m *Model) (*Inputs, error) {
	log.Printf("Processing inputs...")
	i := new(Inputs)
	i.config = new(Config)
	base := map[string]interface{}{}
	currentMap := map[string]interface{}{}
	// Parse chart
	switch {
	case m.Chart != nil:
		// Check if chart is remote url
		u, err := url.Parse(*m.Chart)
		if err != nil {
			return nil, genericError("Process chart", err)
		}
		switch {
		case u.Host != "":
			i.config.repoType = aws.String("Local")
			i.config.chart = aws.String(chartLocalPath)
			i.config.chartPath = m.Chart
		default:
			// Get repo name and chart
			sa := strings.Split(*m.Chart, "/")
			switch {
			case len(sa) > 1:
				i.config.repoName = aws.String(sa[0])
				i.config.chartName = aws.String(sa[1])
			default:
				i.config.repoName = aws.String("stable")
				i.config.chartName = m.Chart
			}
			i.config.repoType = aws.String("Remote")
			i.config.chart = aws.String(fmt.Sprintf("%s/%s", *i.config.repoName, *i.config.chartName))
		}
	default:
		return nil, errors.New("Chart is required")
	}
	if m.Values != nil {
		for _, str := range m.Values {
			if err := strvals.ParseInto(str, base); err != nil {
				return nil, genericError("Process values", err)
			}
		}
	}
	switch {
	case m.Namespace != nil:
		i.config.namespace = m.Namespace
	default:
		i.config.namespace = aws.String("default")
	}
	if m.Version != nil {
		i.config.version = m.Version
	}
	switch {
	case m.Repository != nil:
		i.config.repoURL = m.Repository
	default:
		i.config.repoURL = aws.String(stableRepoURL)
	}
	switch {
	case m.Name != nil:
		i.config.name = m.Name
	default:
		i.config.name = aws.String(*i.config.chartName + "-" + fmt.Sprintf("%d", time.Now().Unix()))
	}
	if m.ValueOverrideURL != nil {
		u, err := url.Parse(*m.ValueOverrideURL)
		if err != nil {
			return nil, genericError("Process ValueOverrideURL ", err)
		}
		bucket := u.Host
		key := strings.TrimLeft(u.Path, "/")
		err = downloadS3(c.session, bucket, key, valuesYamlFile)
		if err != nil {
			return nil, err
		}
		byteKey, err := ioutil.ReadFile(valuesYamlFile)
		if err != nil {
			return nil, genericError("Reading custom yaml", err)
		}
		if err := yaml.Unmarshal(byteKey, &currentMap); err != nil {
			return nil, genericError("Parsing yaml", err)
		}
	}
	// Merge with the maps
	i.valueOpts = mergeMaps(base, currentMap)
	log.Printf("Processing inputs completed!")
	return i, nil
}

//AWSError takes an AWS generated error and handles it
func AWSError(err error) error {
	if awsErr, ok := err.(awserr.Error); ok {
		// Get error details
		log.Printf("AWS Error: %s - %s %v\n", awsErr.Code(), awsErr.Message(), awsErr.OrigErr())

		// Prints out full error message, including original error if there was one.
		log.Printf("Error: %v", awsErr.Error())

		// Get original error
		if origErr := awsErr.OrigErr(); origErr != nil {
			// operate on original error.
		}
		return fmt.Errorf("AWS Error: %s - %s %v", awsErr.Code(), awsErr.Message(), awsErr.OrigErr())
	}
	return fmt.Errorf(err.Error())
}

//genericError takes  error, log it and return new err.
func genericError(source string, err error) error {
	log.Printf("Error: At %s - %s \n", source, err)
	return fmt.Errorf("Error: At %s - %s ", source, err)
}

// Merge values maps
func mergeMaps(a, b map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(a))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		if v, ok := v.(map[string]interface{}); ok {
			if bv, ok := out[k]; ok {
				if bv, ok := bv.(map[string]interface{}); ok {
					out[k] = mergeMaps(bv, v)
					continue
				}
			}
		}
		out[k] = v
	}
	return out
}

// downloadHTTP downloads the file to specified path
func downloadHTTP(url string, filepath string) error {
	log.Printf("Getting file from URL...")
	// Get the data
	resp, err := http.Get(url)
	if err != nil {
		return genericError("Downloading file", err)
	}
	defer resp.Body.Close()

	// Create the file
	out, err := os.Create(filepath)
	if err != nil {
		return genericError("Creating file", err)
	}
	defer out.Close()

	// Write the body to file
	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return genericError("Writing file", err)
	}
	return nil
}

//generateID is to generate physical id for CFN
func generateID(m *Model, name string, region string, namespace string) (*string, error) {
	i := &ID{}
	switch {
	case m.ClusterID != nil:
		i.ClusterID = *m.ClusterID
	case m.KubeConfig != nil:
		i.KubeConfig = *m.KubeConfig
	default:
		return nil, errors.New("Either ClusterID or KubeConfig must be specified")
	}
	i.Name = name
	i.Namespace = namespace
	i.Region = region
	out, err := json.Marshal(i)
	if err != nil {
		return nil, genericError("Json Marshal", err)
	}
	str := base64.RawURLEncoding.EncodeToString(out)
	return aws.String(str), nil
}

//decodeID decodes the physical id provided by CFN
func decodeID(id *string) (*ID, error) {
	i := &ID{}
	str, err := base64.RawURLEncoding.DecodeString(*id)
	if err != nil {
		return nil, genericError("Decode", err)
	}
	err = json.Unmarshal([]byte(str), i)
	if err != nil {
		return nil, genericError("Json Unmarshal", err)
	}
	return i, nil
}
