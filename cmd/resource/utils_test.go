package resource

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/stretchr/testify/assert"
)

type TestDetailParam struct {
	ID int
}

type TestDetailSubStructure struct {
	ID     int
	Params []TestDetailParam
}

type TestDetail struct {
	ID     int
	Detail Detail
	Data   TestDetailSubStructure
}

type Detail interface{}

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

func TestProcessValues(t *testing.T) {
	stringYaml := `root:
  firstlevel: value
  secondlevel:
    - a1
    - a2
  string: true`

	m := &Model{
		Values:    map[string]string{"stack": "true"},
		ValueYaml: aws.String(stringYaml),
		//ValueOverrideURL: aws.String("s3://test/test.yaml"),
	}

	eRes := map[string]interface{}{"root": map[string]interface{}{"firstlevel": "value", "secondlevel": []interface{}{"a1", "a2"}, "string": true}, "stack": "true"}
	data, _ := ioutil.ReadFile(TestFolder + "/test.yaml")
	_, _ = dlLoggingSvcNoChunk(data)

	c := NewMockClient(t)
	result, err := c.processValues(m)
	assert.Nil(t, err)
	assert.EqualValues(t, eRes, result)

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
	testServer := httptest.NewServer(http.StripPrefix("/", http.FileServer(http.Dir(TestFolder))))
	defer func() { testServer.Close() }()
	for _, file := range files {
		t.Run(file, func(t *testing.T) {

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

func TestZero(t *testing.T) {
	one, zeroInt := 1, 0

	type myString string

	var interface1, interfaceZero interface{} = &one, &zeroInt

	var (
		zeroDetail1 Detail = &struct{}{}
		zeroDetail2 Detail = &TestDetail{}
		zeroDetail3 Detail = struct{}{}
		zeroDetail4 Detail = &TestDetail{}
		zeroDetail5 Detail = &TestDetail{Data: TestDetailSubStructure{Params: nil}}
		zeroDetail6 Detail = &TestDetail{Data: TestDetailSubStructure{
			Params: make([]TestDetailParam, 0, 10)},
		}

		nonZeroDetail1 Detail = &TestDetail{Data: TestDetailSubStructure{
			Params: []TestDetailParam{TestDetailParam{55}}},
		}
		nonZeroDetail2 Detail = &TestDetail{Data: TestDetailSubStructure{ID: 1234}}
		nonZeroDetail3 Detail = &TestDetail{ID: 1234}
		nonZeroDetail4 Detail = &TestDetail{Detail: nonZeroDetail3}
	)

	for i, test := range []struct {
		v    interface{}
		want bool
	}{
		// basic types
		{0, true},
		{complex(0, 0), true},
		{1, false},
		{1.0, false},
		{true, false},
		{0.0, true},
		{"foo", false},
		{"", true},
		{myString(""), true},     // different types
		{myString("foo"), false}, // different types
		// slices
		{[]string{"foo"}, false},
		{[]string(nil), true},
		{[]string{}, true},
		// maps
		{map[string][]int{"foo": {1, 2, 3}}, false},
		{map[string][]int{"foo": {1, 2, 3}}, false},
		{map[string][]int{}, true},
		{map[string][]int(nil), true},
		// pointers
		{&one, false},
		{&zeroInt, true},
		{new(bytes.Buffer), true},
		// arrays
		{[...]int{1, 2, 3}, false},

		// interfaces
		{&interface1, false},
		{&interfaceZero, true},
		// special case for structures
		{zeroDetail1, true},
		{zeroDetail2, true},
		{zeroDetail3, true},
		{zeroDetail4, true},
		{zeroDetail5, true},
		{zeroDetail6, true},
		{nonZeroDetail1, false},
		{nonZeroDetail2, false},
		{nonZeroDetail3, false},
		{nonZeroDetail4, false},
	} {
		if IsZero(test.v) != test.want {
			t.Errorf("Zero(%v)[%d] = %t", test.v, i, !test.want)
		}
	}
}
