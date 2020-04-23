package resource

import (
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"regexp"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
)

const (
	testFolder = "testdata"
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
	if reflect.DeepEqual(result, expectedMap) {
		t.Logf("mergeMaps PASSED")
	} else {
		t.Errorf("mergeMaps FAILED expected: %v but got %v", expectedMap, result)
	}

}

// TestHTTPDownload is to test downloadHTTP
func TestHTTPDownload(t *testing.T) {
	files := []string{"test.tar.gz", "nonExt"}
	//expectedRespStatus := 200
	// generate a test server so we can capture and inspect the request
	testServer := httptest.NewServer(http.StripPrefix("/", http.FileServer(http.Dir(testFolder))))
	defer func() { testServer.Close() }()
	for _, file := range files {
		t.Run(file, func(t *testing.T) {
			//req, err := http.NewRequest(http.MethodGet, testServer.URL, nil)
			err := downloadHTTP(testServer.URL+"/"+file, "/dev/null")
			re := regexp.MustCompile(`Error: At Downloading file`)
			if err != nil {
				if re.MatchString(err.Error()) {
					t.Logf("downloadHTTP PASSED")
				} else {
					t.Errorf("downloadHTTP FAILED expected: got error %s", err)
				}
			}
		})
	}
}

// TestGenerateID is to test generateID
func TestGenerateID(t *testing.T) {
	eID := aws.String("eyJDbHVzdGVySUQiOiJla3MiLCJSZWdpb24iOiJldS13ZXN0LTEiLCJOYW1lIjoiVGVzdCIsIk5hbWVzcGFjZSI6IlRlc3QifQ")
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
			namespace:     "Test",
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
			namespace:     "Test",
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
			namespace:     "Test",
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
			namespace:     "Test",
			expectedID:    eID,
			expectedError: "",
		},
	}
	for name, d := range tests {
		t.Run(name, func(t *testing.T) {
			result, err := generateID(&d.m, d.name, d.region, d.namespace)
			switch {
			case err != nil:
				if err.Error() == d.expectedError {
					t.Logf("generateID PASSED")
				} else {
					t.Errorf("generateID FAILED expected error : \"%s\" but got \"%s\"", d.expectedError, err.Error())
				}
			case *result == *d.expectedID:
				t.Logf("generateID PASSED")

			default:
				t.Errorf("generateID FAILED expected: %s but got %s", *d.expectedID, *result)
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
	if reflect.DeepEqual(result, eID) {
		t.Logf("DecodeID PASSED")
	} else {
		t.Errorf("DecodeID FAILED expected: %v but got %v", eID, result)
	}
}

// TestCheckTimeOut to test checkTimeOut
func TestCheckTimeOut(t *testing.T) {
	timeOut := aws.Int(90)
	tests := map[string]struct {
		time     string
		expected bool
	}{
		"10M": {
			time:     time.Now().Add(time.Minute * -10).Format(time.RFC3339),
			expected: false,
		},
		"10H": {
			time:     time.Now().Add(time.Hour * -10).Format(time.RFC3339),
			expected: true,
		},
	}
	for name, d := range tests {
		t.Run(name, func(t *testing.T) {
			result := checkTimeOut(d.time, timeOut)
			if result == d.expected {
				t.Logf("checkTimeOut PASSED")
			} else {
				t.Errorf("checkTimeOut FAILED expected: %v but got %v", d.expected, result)
			}
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
			result := getStage(d.context)
			if result == d.expectedStage && os.Getenv("StartTime") == d.expectedTime {
				t.Logf("getStage PASSED")
			} else {
				t.Errorf("getStage FAILED expected: %s,%s but got %s,%s", d.expectedStage, d.expectedTime, result, os.Getenv("StartTime"))
			}
		})
	}
}

// TestHash is to test getHash
func TestHash(t *testing.T) {
	str := "Test"
	expectedHash := aws.String("0cbc6611f5540bd0809a388dc95a615b")
	result := getHash(str)
	if *expectedHash == *result {
		t.Logf("getHash PASSED")
	} else {
		t.Errorf("getHash FAILED expected: %s but got %s", *expectedHash, *result)
	}
}
