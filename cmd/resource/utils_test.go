package resource

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/stretchr/testify/assert"
)

// TestMergeMaps is to test MergeMaps
func TestMergeMaps(t *testing.T) {
	m1 := map[string]interface{}{
		"a": "a",
		"b": "a",
	}
	m2 := map[string]interface{}{
		"a": "b",
		"b": "b",
		"c": "c",
	}
	expectedMap := map[string]interface{}{
		"a": "b",
		"b": "b",
		"c": "c",
	}
	result := mergeMaps(m1, m2)
	assert.EqualValues(t, expectedMap, result)
}

// TestGetChartDetails is to test getChartDetails
func TestGetChartDetails(t *testing.T) {
	tests := map[string]struct {
		m             *Model
		expectedChart *Chart
		expectedError *string
	}{
		"test1": {
			m: &Model{
				Chart:      aws.String("stable/test"),
				Repository: aws.String("test.com"),
			},
			expectedChart: &Chart{
				Chart:        aws.String("stable/test"),
				ChartRepo:    aws.String("stable"),
				ChartName:    aws.String("test"),
				ChartType:    aws.String("Remote"),
				ChartRepoURL: aws.String("test.com"),
			},
			expectedError: nil,
		},
		"test2": {
			m: &Model{
				Repository: aws.String("test.com"),
			},
			expectedChart: &Chart{},
			expectedError: aws.String("Chart is required"),
		},
		"test3": {
			m: &Model{
				Chart:   aws.String("test"),
				Version: aws.String("1.0.0"),
			},
			expectedChart: &Chart{
				Chart:        aws.String("stable/test"),
				ChartRepo:    aws.String("stable"),
				ChartName:    aws.String("test"),
				ChartType:    aws.String("Remote"),
				ChartRepoURL: aws.String("https://kubernetes-charts.storage.googleapis.com"),
				ChartVersion: aws.String("1.0.0"),
			},
			expectedError: nil,
		},
	}
	for name, d := range tests {
		t.Run(name, func(t *testing.T) {
			result, err := getChartDetails(d.m)
			if err != nil {
				assert.EqualError(t, err, aws.StringValue(d.expectedError))
			} else {
				assert.EqualValues(t, d.expectedChart, result)
			}
		})
	}
}

// TestGetReleaseName is to test getReleaseName
func TestGetReleaseName(t *testing.T) {
	tests := map[string]struct {
		name         *string
		chartname    *string
		expectedName *string
	}{
		"NameProvided": {
			name:         aws.String("Test"),
			chartname:    nil,
			expectedName: aws.String("Test"),
		},
		"AllValues": {
			name:         aws.String("Test"),
			chartname:    aws.String("TestChart"),
			expectedName: aws.String("Test"),
		},
		"OnlyChart": {
			name:         nil,
			chartname:    aws.String("TestChart"),
			expectedName: aws.String("TestChart-" + fmt.Sprintf("%d", time.Now().Unix())),
		},
		"NoValues": {
			name:         nil,
			chartname:    nil,
			expectedName: nil,
		},
	}
	for name, d := range tests {
		t.Run(name, func(t *testing.T) {
			result := getReleaseName(d.name, d.chartname)
			assert.EqualValues(t, aws.StringValue(d.expectedName), aws.StringValue(result))
		})
	}
}

// TestGetReleaseNameContextis to test getReleaseNameContext
func TestGetReleaseNameContext(t *testing.T) {
	tests := map[string]struct {
		context      map[string]interface{}
		expectedName *string
	}{
		"NameProvided": {
			context:      map[string]interface{}{"Name": "Test"},
			expectedName: aws.String("Test"),
		},
		"Nil": {
			context:      map[string]interface{}{},
			expectedName: nil,
		},
		"NoValues": {
			context:      map[string]interface{}{"StartTime": "Testtime"},
			expectedName: nil,
		},
	}
	for name, d := range tests {
		t.Run(name, func(t *testing.T) {
			result := getReleaseNameContext(d.context)
			assert.EqualValues(t, aws.StringValue(d.expectedName), aws.StringValue(result))
		})
	}
}

// TestGetReleaseNameSpace is to test getReleaseNameSpace
func TestGetReleaseNameSpace(t *testing.T) {
	tests := map[string]struct {
		namespace         *string
		expectedNamespace *string
	}{
		"NameProvided": {
			namespace:         aws.String("default"),
			expectedNamespace: aws.String("default"),
		},
		"NoValues": {
			namespace:         nil,
			expectedNamespace: aws.String("default"),
		},
	}
	for name, d := range tests {
		t.Run(name, func(t *testing.T) {
			result := getReleaseNameSpace(d.namespace)
			assert.EqualValues(t, aws.StringValue(d.expectedNamespace), aws.StringValue(result))
		})
	}
}

// TestHTTPDownload is to test downloadHTTP
func TestHTTPDownload(t *testing.T) {
	files := []string{"test.tgz", "nonExt"}
	//expectedRespStatus := 200
	// generate a test server so we can capture and inspect the request
	testServer := MakeTestServer(TestFolder)
	for _, file := range files {
		t.Run(file, func(t *testing.T) {
			//req, err := http.NewRequest(http.MethodGet, testServer.URL, nil)
			err := downloadHTTP(testServer.URL+"/"+file, "/dev/null")
			if err != nil {
				assert.Contains(t, err.Error(), "At Downloading file")
			}
		})
	}
}

// TestGenerateID is to test generateID
func TestGenerateID(t *testing.T) {
	eID := aws.String("eyJDbHVzdGVySUQiOiJla3MiLCJSZWdpb24iOiJldS13ZXN0LTEiLCJOYW1lIjoiVGVzdCIsIk5hbWVzcGFjZSI6ImRlZmF1bHQifQ")
	tests := map[string]struct {
		m                                      Model
		name, region, namespace, expectedError string
		expectedID                             *string
	}{
		"WithAllValues": {
			m: Model{
				ClusterID:  aws.String("eks"),
				KubeConfig: aws.String("arn"),
			},
			name:          "Test",
			region:        "eu-west-1",
			namespace:     "default",
			expectedID:    eID,
			expectedError: "Both ClusterID or KubeConfig can not be specified",
		},
		"NoModelValues": {
			m: Model{
				ClusterID:  nil,
				KubeConfig: nil,
			},
			name:          "Test",
			region:        "eu-west-1",
			namespace:     "default",
			expectedID:    eID,
			expectedError: "Either ClusterID or KubeConfig must be specified",
		},
		"BlankName": {
			m: Model{
				ClusterID:  aws.String("eks"),
				KubeConfig: nil,
			},
			name:          "",
			region:        "eu-west-1",
			namespace:     "default",
			expectedID:    eID,
			expectedError: "Incorrect values for variable name, namespace, region",
		},
		"BlankValues": {
			m: Model{
				ClusterID:  nil,
				KubeConfig: nil,
			},
			name:          "",
			region:        "",
			namespace:     "",
			expectedID:    eID,
			expectedError: "Either ClusterID or KubeConfig must be specified",
		},
		"CorrectValues": {
			m: Model{
				ClusterID:  aws.String("eks"),
				KubeConfig: nil,
			},
			name:          "Test",
			region:        "eu-west-1",
			namespace:     "default",
			expectedID:    eID,
			expectedError: "",
		},
	}
	for name, d := range tests {
		t.Run(name, func(t *testing.T) {
			result, err := generateID(&d.m, d.name, d.region, d.namespace)
			if err != nil {
				assert.EqualError(t, err, d.expectedError)
			} else {
				assert.EqualValues(t, aws.StringValue(d.expectedID), aws.StringValue(result))
			}
		})
	}
}

// TestDecodeID is to test DecodeID
func TestDecodeID(t *testing.T) {
	sID := aws.String("eyJDbHVzdGVySUQiOiJla3MiLCJSZWdpb24iOiJldS13ZXN0LTEiLCJOYW1lIjoiVGVzdCIsIk5hbWVzcGFjZSI6IlRlc3QifQ")
	eID := &ID{
		ClusterID: "eks",
		Name:      "Test",
		Region:    "eu-west-1",
		Namespace: "Test",
	}
	result, _ := DecodeID(sID)
	assert.EqualValues(t, eID, result)
}

// TestCheckTimeOut to test checkTimeOut
func TestCheckTimeOut(t *testing.T) {
	timeOut := aws.Int(90)
	tests := map[string]struct {
		time      string
		assertion assert.BoolAssertionFunc
	}{
		"10M": {
			time:      time.Now().Add(time.Minute * -10).Format(time.RFC3339),
			assertion: assert.False,
		},
		"10H": {
			time:      time.Now().Add(time.Hour * -10).Format(time.RFC3339),
			assertion: assert.True,
		},
	}
	for name, d := range tests {
		t.Run(name, func(t *testing.T) {
			result := checkTimeOut(d.time, timeOut)
			d.assertion(t, result)
		})
	}
}

// TestGetStage is to test getStage
func TestGetStage(t *testing.T) {
	st := time.Now().Format(time.RFC3339)
	tests := map[string]struct {
		context       map[string]interface{}
		expectedStage Stage
		expectedTime  string
	}{
		"Init": {
			context:       make(map[string]interface{}),
			expectedStage: InitStage,
			expectedTime:  st,
		},
		"Stage": {
			context: map[string]interface{}{
				"Stage":     "ReleaseStabilize",
				"StartTime": st,
			},
			expectedStage: ReleaseStabilize,
			expectedTime:  st,
		},
		"StageNotime": {
			context: map[string]interface{}{
				"Stage": "ReleaseStabilize",
			},
			expectedStage: ReleaseStabilize,
			expectedTime:  st,
		},
		"TimeNoStage": {
			context: map[string]interface{}{
				"StartTime": st,
			},
			expectedStage: InitStage,
			expectedTime:  st,
		},
	}
	for name, d := range tests {
		t.Run(name, func(t *testing.T) {
			os.Setenv("StartTime", d.expectedTime)
			result := getStage(d.context)
			assert.EqualValues(t, d.expectedStage, result)
			assert.EqualValues(t, d.expectedTime, os.Getenv("StartTime"))
		})
	}
}

// TestHash is to test getHash
func TestHash(t *testing.T) {
	str := "Test"
	expectedHash := aws.String("0cbc6611f5540bd0809a388dc95a615b")
	result := getHash(str)
	assert.EqualValues(t, aws.StringValue(expectedHash), aws.StringValue(result))
}
