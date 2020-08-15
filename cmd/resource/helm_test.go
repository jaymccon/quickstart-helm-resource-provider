package resource

import (
	"helm.sh/helm/v3/pkg/cli"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/stretchr/testify/assert"
	"helm.sh/helm/v3/pkg/repo"
)

func TestHelmClientInvoke(t *testing.T) {
	setting := cli.New()
	_, err := helmClientInvoke(aws.String("default"), setting.RESTClientGetter())
	assert.Nil(t, err)
}

// TestAddHelmRepoUpdate to test addHelmRepoUpdate
func TestAddHelmRepoUpdate(t *testing.T) {
	c := NewMockClient(t, nil)
	defer os.Remove(c.Settings.RepositoryConfig)
	tests := map[string]struct {
		name        string
		url         string
		eCount      int
		expectedErr *string
	}{
		"StableRepo": {
			name:   "stable",
			url:    "https://kubernetes-charts.storage.googleapis.com",
			eCount: 1,
		},
		"WrongRepo": {
			name:        "stable",
			url:         "https://test.com",
			expectedErr: aws.String("is not a valid chart repository"),
		},
	}
	for name, d := range tests {
		t.Run(name, func(t *testing.T) {
			err := addHelmRepoUpdate(d.name, d.url, c.Settings)
			if err != nil {
				assert.Contains(t, err.Error(), aws.StringValue(d.expectedErr))
			} else {
				r, _ := repo.LoadFile(c.Settings.RepositoryConfig)
				assert.Equal(t, d.eCount, len(r.Repositories))
			}
		})
	}
}

// TestHelmInstall to test HelmInstall
func TestHelmInstall(t *testing.T) {
	defer os.Remove(chartLocalPath)
	testServer := httptest.NewServer(http.StripPrefix("/", http.FileServer(http.Dir(TestFolder))))
	defer func() { testServer.Close() }()
	c := NewMockClient(t, nil)
	tests := map[string]struct {
		m           *Model
		config      *Config
		vals        map[string]interface{}
		expectedErr *string
	}{
		"HTTPRepo": {
			m: &Model{Chart: aws.String(testServer.URL + "/test.tgz")},
			config: &Config{
				Name:      aws.String("HTTPRepo"),
				Namespace: aws.String("default"),
			},
		},
		"WrongChartFile": {
			m: &Model{Chart: aws.String(testServer.URL + "/testt.tgz")},
			config: &Config{
				Name:      aws.String("test"),
				Namespace: aws.String("default"),
			},
			expectedErr: aws.String("At Downloading file"),
		},
		"RemoteRepo": {
			m: &Model{Chart: aws.String("stable/coscale")},
			config: &Config{
				Name:      aws.String("RemoteRepo"),
				Namespace: aws.String("default"),
			},
		},
		"WrongRemoteRepo": {
			m: &Model{Chart: aws.String("test/test")},
			config: &Config{
				Name:      aws.String("test"),
				Namespace: aws.String("default"),
			},
			expectedErr: aws.String("failed to download"),
		},
		"Dependency": {
			m: &Model{Chart: aws.String(testServer.URL + "/dep-0.1.0.tgz")},
			config: &Config{
				Name:      aws.String("Dependency"),
				Namespace: aws.String("default"),
			},
		},
	}

	for name, d := range tests {
		t.Run(name, func(t *testing.T) {
			ch, _ := getChartDetails(d.m)
			err := c.HelmInstall(d.config, d.vals, ch)
			if err != nil {
				assert.Contains(t, err.Error(), aws.StringValue(d.expectedErr))
			}
		})
	}
}

// TestHelmUninstall to test HelmUninstall
func TestHelmUninstall(t *testing.T) {
	expectedErr := "not found"
	c := NewMockClient(t, nil)
	releases := []string{"one", "five"}
	for _, rel := range releases {
		t.Run(rel, func(t *testing.T) {
			err := c.HelmUninstall(rel)
			if err != nil {
				assert.Contains(t, err.Error(), expectedErr)
			}
		})
	}
}

// TestHelmStatus to test HelmStatus
func TestHelmStatus(t *testing.T) {
	c := NewMockClient(t, nil)
	tests := map[string]struct {
		name        string
		eStatus     *HelmStatusData
		expectedErr *string
	}{
		"Deployed": {
			name: "one",
			eStatus: &HelmStatusData{
				Chart:        "hello-0.1.0",
				ChartName:    "hello",
				Status:       "deployed",
				Namespace:    "default",
				ChartVersion: "0.1.0",
				Manifest:     TestManifest,
			},
		},
		"NonExt": {
			name:        "nonext",
			expectedErr: aws.String("not found"),
		},
	}
	for name, d := range tests {
		t.Run(name, func(t *testing.T) {
			h, err := c.HelmStatus(d.name)
			if err != nil {
				assert.Contains(t, err.Error(), aws.StringValue(d.expectedErr))
			} else {
				assert.EqualValues(t, d.eStatus, h)
			}
		})
	}
}

// TestHelmList to test HelmList
func TestHelmList(t *testing.T) {
	c := NewMockClient(t, nil)
	hl := []HelmListData{}
	for _, rel := range []string{"one", "two", "three", "five"} {
		l := HelmListData{ReleaseName: rel, ChartName: "hello", ChartVersion: "0.1.0", Chart: "hello-0.1.0", Namespace: "default"}
		hl = append(hl, l)
	}
	tests := map[string]struct {
		chart       *Chart
		config      *Config
		eList       []HelmListData
		expectedErr *string
	}{
		"Chart": {
			chart: &Chart{
				Chart:        aws.String("hello-0.1.0"),
				ChartName:    aws.String("hello"),
				ChartVersion: aws.String("0.1.0"),
			},
			config: &Config{
				Name:      aws.String("test"),
				Namespace: aws.String("default"),
			},
			eList:       hl,
			expectedErr: aws.String("test"),
		},
	}
	for name, d := range tests {
		t.Run(name, func(t *testing.T) {
			h, err := c.HelmList(d.config, d.chart)
			if err != nil {
				assert.Contains(t, err.Error(), aws.StringValue(d.expectedErr))
			} else {
				assert.ElementsMatch(t, d.eList, h)
			}
		})
	}
}

// TestHelmUpgrade to test HelmUpgrade
func TestHelmUpgrade(t *testing.T) {
	defer os.Remove(chartLocalPath)
	testServer := httptest.NewServer(http.StripPrefix("/", http.FileServer(http.Dir(TestFolder))))
	defer func() { testServer.Close() }()
	c := NewMockClient(t, nil)
	tests := map[string]struct {
		m           *Model
		config      *Config
		vals        map[string]interface{}
		expectedErr *string
	}{
		"HTTPRepo": {
			m: &Model{Chart: aws.String(testServer.URL + "/test.tgz")},
			config: &Config{
				Name:      aws.String("test"),
				Namespace: aws.String("default"),
			},
		},
		"Dependency": {
			m: &Model{Chart: aws.String(testServer.URL + "/dep-0.1.0.tgz")},
			config: &Config{
				Name:      aws.String("test"),
				Namespace: aws.String("default"),
			},
		},
		"WrongChartFile": {
			m: &Model{Chart: aws.String(testServer.URL + "/testt.tgz")},
			config: &Config{
				Name:      aws.String("test"),
				Namespace: aws.String("default"),
			},
			expectedErr: aws.String("At Downloading file"),
		},
		"RemoteRepo": {
			m: &Model{Chart: aws.String("stable/coscale")},
			config: &Config{
				Name:      aws.String("test"),
				Namespace: aws.String("default"),
			},
		},
		"WrongRemoteRepo": {
			m: &Model{Chart: aws.String("test/test")},
			config: &Config{
				Name:      aws.String("test"),
				Namespace: aws.String("default"),
			},
			expectedErr: aws.String("failed to download"),
		},
	}

	for name, d := range tests {
		t.Run(name, func(t *testing.T) {
			ch, _ := getChartDetails(d.m)
			err := c.HelmUpgrade(aws.StringValue(d.config.Name), d.config, d.vals, ch)
			if err != nil {
				assert.Contains(t, err.Error(), aws.StringValue(d.expectedErr))
			}
		})
	}
}
