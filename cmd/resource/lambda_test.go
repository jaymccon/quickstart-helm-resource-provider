package resource

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/lambda"
	"github.com/stretchr/testify/assert"
	"helm.sh/helm/v3/pkg/release"
)

// Define mock structs.
type mockLambdaClient struct {
	LambdaAPI
}

func (m *mockLambdaClient) CreateFunction(*lambda.CreateFunctionInput) (*lambda.FunctionConfiguration, error) {
	return nil, nil
}

func (m *mockLambdaClient) DeleteFunction(i *lambda.DeleteFunctionInput) (*lambda.DeleteFunctionOutput, error) {
	if aws.StringValue(i.FunctionName) == "function1" {
		return nil, nil
	}
	return nil, awserr.New(lambda.ErrCodeResourceNotFoundException, "NotFound", fmt.Errorf("NotFound"))
}

func (m *mockLambdaClient) GetFunction(i *lambda.GetFunctionInput) (*lambda.GetFunctionOutput, error) {
	if aws.StringValue(i.FunctionName) == "function1" {
		return &lambda.GetFunctionOutput{
			Configuration: &lambda.FunctionConfiguration{
				State:      aws.String("Active"),
				CodeSha256: aws.String("uf/I8HjgfTQppww/TIP7VeKvnAh0ce24g+3l9JhBpzo="),
			},
		}, nil
	}
	if aws.StringValue(i.FunctionName) == "function2" {
		return &lambda.GetFunctionOutput{
			Configuration: &lambda.FunctionConfiguration{
				State: aws.String("Failed"),
			},
		}, nil
	}
	if aws.StringValue(i.FunctionName) == "function3" {
		return &lambda.GetFunctionOutput{
			Configuration: &lambda.FunctionConfiguration{
				State:      aws.String("Active"),
				CodeSha256: aws.String("xf/I8HjgfTQqqww/TIP7VeKvnAh0ce24g+3l9JhBpzo="),
			},
		}, nil
	}
	if aws.StringValue(i.FunctionName) == "helm-provider-vpc-connector-0458f313f181167f7e501510610dcbd4" {
		return &lambda.GetFunctionOutput{
			Configuration: &lambda.FunctionConfiguration{
				State:      aws.String("Active"),
				CodeSha256: aws.String("uf/I8HjgfTQppww/TIP7VeKvnAh0ce24g+3l9JhBpzo="),
			},
		}, nil
	}
	if aws.StringValue(i.FunctionName) == "helm-provider-vpc-connector-38919e8bbd92924c6d275cf1409ff027" {
		return &lambda.GetFunctionOutput{
			Configuration: &lambda.FunctionConfiguration{
				State:      aws.String("Pending"),
				CodeSha256: aws.String("uf/I8HjgfTQppww/TIP7VeKvnAh0ce24g+3l9JhBpzo="),
			},
		}, nil
	}
	return nil, awserr.New(lambda.ErrCodeResourceNotFoundException, "NotFound", fmt.Errorf("NotFound"))
}

func (m *mockLambdaClient) UpdateFunctionCode(*lambda.UpdateFunctionCodeInput) (*lambda.FunctionConfiguration, error) {
	return nil, nil
}

func (m *mockLambdaClient) UpdateFunctionConfiguration(*lambda.UpdateFunctionConfigurationInput) (*lambda.FunctionConfiguration, error) {
	return nil, nil
}

func (m *mockLambdaClient) Invoke(i *lambda.InvokeInput) (*lambda.InvokeOutput, error) {
	if aws.StringValue(i.FunctionName) == "function2" {
		t := map[string]string{"errorType": "SomeType", "errorMessage": "SomeMessage"}
		p, _ := json.Marshal(t)
		return &lambda.InvokeOutput{
			FunctionError: aws.String("Function error"),
			Payload:       p,
		}, nil
	}
	r, _ := json.Marshal(&LambdaResponse{
		StatusData: &HelmStatusData{
			Status:    release.StatusDeployed,
			Namespace: "default",
			Manifest:  TestManifest,
		},
		PendingResources: false,
	})

	//r, _ := json.Marshal(&LambdaResponse{})

	return &lambda.InvokeOutput{
		Payload: r,
	}, nil
}

// TestCreateFunction to test createFunction
func TestCreateFunction(t *testing.T) {
	eErr := "no such file or directory"
	mockSvc := &mockLambdaClient{}
	vpc := &VPCConfiguration{
		SecurityGroupIds: []string{"sg-1"},
		SubnetIds:        []string{"subnet-1"},
	}
	tests := map[string]struct {
		lr *lambdaResource
	}{
		"Correct": {
			lr: &lambdaResource{
				nameSuffix:   aws.String("suffix"),
				functionFile: TestZipFile,
				vpcConfig:    vpc,
			},
		},
		"IncorrectZip": {
			lr: &lambdaResource{
				nameSuffix:   aws.String("suffix"),
				functionFile: "/noExr",
				vpcConfig:    vpc,
			},
		},
	}
	for name, d := range tests {
		t.Run(name, func(t *testing.T) {
			err := createFunction(mockSvc, d.lr)
			if err != nil {
				assert.Contains(t, err.Error(), eErr)
			}
		})
	}
}

// TestDeleteFunction to test deleteFunction
func TestDeleteFunction(t *testing.T) {
	mockSvc := &mockLambdaClient{}
	functions := []string{"function1", "function2"}
	for _, f := range functions {
		t.Run(f, func(t *testing.T) {
			err := deleteFunction(mockSvc, aws.String(f))
			assert.Nil(t, err)
		})
	}
}

// TestGetFunction to test getFunction
func TestGetFunction(t *testing.T) {
	mockSvc := &mockLambdaClient{}
	tests := map[string]struct {
		name *string
		eRes *lambda.GetFunctionOutput
		eErr *string
	}{
		"Correct": {
			name: aws.String("function1"),
			eRes: &lambda.GetFunctionOutput{
				Configuration: &lambda.FunctionConfiguration{
					CodeSha256: aws.String("uf/I8HjgfTQppww/TIP7VeKvnAh0ce24g+3l9JhBpzo="),
					State:      aws.String("Active"),
				},
			},
		},
		"NoExt": {
			name: aws.String("Nofunct"),
			eRes: &lambda.GetFunctionOutput{},
			eErr: aws.String("NotFound"),
		},
	}
	for name, d := range tests {
		t.Run(name, func(t *testing.T) {
			result, err := getFunction(mockSvc, d.name)
			if err != nil {
				assert.Contains(t, err.Error(), aws.StringValue(d.eErr))
			} else {
				assert.EqualValues(t, d.eRes, result)
			}
		})
	}
}

// TestUpdateFunction to test updateFunction
func TestUpdateFunction(t *testing.T) {
	mockSvc := &mockLambdaClient{}
	vpc := &VPCConfiguration{
		SecurityGroupIds: []string{"sg-1"},
		SubnetIds:        []string{"subnet-1"},
	}
	tests := map[string]struct {
		lr *lambdaResource
	}{
		"Correct": {
			lr: &lambdaResource{
				functionName: aws.String("function1"),
				functionFile: TestZipFile,
				vpcConfig:    vpc,
			},
		},
		"CodeChange": {
			lr: &lambdaResource{
				functionName: aws.String("function3"),
				functionFile: TestZipFile,
				vpcConfig:    vpc,
			},
		},
	}
	for name, d := range tests {
		t.Run(name, func(t *testing.T) {
			d.lr.functionOutput, _ = getFunction(mockSvc, d.lr.functionName)
			err := updateFunction(mockSvc, d.lr)
			assert.Nil(t, err)
		})
	}
}

// TestChecklambdaState to test checklambdaState
func TestChecklambdaState(t *testing.T) {
	mockSvc := &mockLambdaClient{}
	tests := map[string]struct {
		name   *string
		eState State
	}{
		"StateActive": {
			name:   aws.String("function1"),
			eState: StateActive,
		},
		"StateFailed": {
			name:   aws.String("function2"),
			eState: StateFailed,
		},
		"StateNotFound": {
			name:   aws.String("Nofunct"),
			eState: StateNotFound,
		},
	}
	for name, d := range tests {
		t.Run(name, func(t *testing.T) {
			result, err := checklambdaState(mockSvc, d.name)
			assert.Nil(t, err)
			assert.EqualValues(t, d.eState, result)
		})
	}
}

// TestInvokeLambda to test invokeLambda
func TestInvokeLambda(t *testing.T) {
	mockSvc := &mockLambdaClient{}
	expectedErr := "SomeMessage"
	event := &Event{
		Action: CheckReleaseAction,
	}
	functions := []string{"function1", "function2"}

	for _, name := range functions {
		t.Run(name, func(t *testing.T) {
			_, err := invokeLambda(mockSvc, aws.String(name), event)
			t.Log(err)
			if err != nil {
				assert.Contains(t, err.Error(), expectedErr)
			}
		})
	}
}

// TestGetZip to test getZip
func TestGetZip(t *testing.T) {
	tests := map[string]struct {
		file  string
		eHash string
		eErr  *string
	}{
		"Correct": {
			file:  TestZipFile,
			eHash: "uf/I8HjgfTQppww/TIP7VeKvnAh0ce24g+3l9JhBpzo=",
		},
		"Wrongfile": {
			file: "/nonExt",
			eErr: aws.String("no such file or directory"),
		},
	}
	for name, d := range tests {
		t.Run(name, func(t *testing.T) {
			_, hash, err := getZip(d.file)
			if err != nil {
				assert.Contains(t, err.Error(), aws.StringValue(d.eErr))
			} else {
				assert.EqualValues(t, d.eHash, hash)
			}
		})
	}
}

// TestFunctionNotExists to test functionNotExists
func TestFunctionNotExists(t *testing.T) {
	tests := map[string]struct {
		err       error
		assertion assert.BoolAssertionFunc
	}{
		"NotFound": {
			err:       awserr.New(lambda.ErrCodeResourceNotFoundException, "NotFound", fmt.Errorf("NotFound")),
			assertion: assert.True,
		},
		"OtherErrors": {
			err:       awserr.New(lambda.ErrCodeEC2AccessDeniedException, "NotFound", fmt.Errorf("NotFound")),
			assertion: assert.False,
		},
	}
	for name, d := range tests {
		t.Run(name, func(t *testing.T) {
			result := functionNotExists(d.err)
			d.assertion(t, result)
		})
	}
}

// TestNewLambdaResource to test newLambdaResource
func TestNewLambdaResource(t *testing.T) {
	mockSvc := &mockSTSClient{}
	v := &VPCConfiguration{
		SecurityGroupIds: []string{"sg-1"},
		SubnetIds:        []string{"subnet-1"},
	}
	tests := map[string]struct {
		cluster, kubeconfig *string
		vpc                 *VPCConfiguration
		elambdaResource     *lambdaResource
	}{
		"WithCluster": {
			cluster: aws.String("eks"),

			vpc: v,
			elambdaResource: &lambdaResource{
				roleArn:      aws.String("arn:aws:iam::1234567890:role/TestRole"),
				nameSuffix:   aws.String("37b6fa0c59ff93e123871e92573b290c"),
				vpcConfig:    v,
				functionName: aws.String("helm-provider-vpc-connector-37b6fa0c59ff93e123871e92573b290c"),
				functionFile: "k8svpc.zip",
			},
		},
		"WithKubeConfig": {
			kubeconfig: aws.String("arn"),
			vpc:        v,
			elambdaResource: &lambdaResource{
				roleArn:      aws.String("arn:aws:iam::1234567890:role/TestRole"),
				nameSuffix:   aws.String("88c81d0fbff37a9cfae847245f69cde9"),
				vpcConfig:    v,
				functionName: aws.String("helm-provider-vpc-connector-88c81d0fbff37a9cfae847245f69cde9"),
				functionFile: "k8svpc.zip",
			},
		},
	}
	for name, d := range tests {
		t.Run(name, func(t *testing.T) {
			result := newLambdaResource(mockSvc, d.cluster, d.kubeconfig, d.vpc)
			assert.EqualValues(t, d.elambdaResource, result)
		})
	}
}
