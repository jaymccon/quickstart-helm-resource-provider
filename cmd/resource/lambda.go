package resource

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/lambda"
)

const (
	ZipFile            string = "k8svpc.zip"
	FunctionNamePrefix string = "helm-provider-vpc-connector-"
	Handler            string = "k8svpc"
	MemorySize         int64  = 256
	Runtime            string = "go1.x"
	Timeout            int64  = 900
)

type Event struct {
	Kubeconfig  []byte       `json:",omitempty"`
	Inputs      *Inputs      `json:",omitempty"`
	ID          *ID          `json:",omitempty"`
	Model       *Model       `json:",omitempty"`
	Action      Action       `json:",omitempty"`
	ReleaseData *ReleaseData `json:",omitempty"`
}

type Action string

const (
	InstallReleaseAction   Action = "InstallRelease"
	UpdateReleaseAction    Action = "UpdateRelease"
	CheckReleaseAction     Action = "CheckRelease"
	GetPendingAction       Action = "GetPending"
	GetResourcesAction     Action = "GetResources"
	UninstallReleaseAction Action = "UninstallRelease"
	ListReleaseAction      Action = "ListRelease"
)

type lambdaResource struct {
	roleArn        *string
	nameSuffix     *string
	vpcConfig      *VPCConfiguration
	functionOutput *lambda.GetFunctionOutput
	functionName   *string
	functionFile   string
	awssession     *session.Session
}

type LambdaResponse struct {
	StatusData       *HelmStatusData        `json:",omitempty"`
	ListData         []HelmListData         `json:",omitempty"`
	Resources        map[string]interface{} `json:",omitempty"`
	PendingResources bool                   `json:",omitempty"`
}

type State string

const (
	StatePending  State = "Pending"
	StateActive   State = "Active"
	StateInactive State = "Inactive"
	StateFailed   State = "Failed"
	StateNotFound State = "NotFound"
)

func createFunction(svc LambdaAPI, l *lambdaResource) error {
	log.Printf("Creating the VPC connector %s", FunctionNamePrefix+*l.nameSuffix)
	zip, _, err := getZip(l.functionFile)
	if err != nil {
		return AWSError(err)
	}
	funcName := FunctionNamePrefix + *l.nameSuffix
	input := &lambda.CreateFunctionInput{
		Code: &lambda.FunctionCode{
			ZipFile: zip,
		},
		FunctionName: aws.String(funcName),
		Handler:      aws.String(Handler),
		MemorySize:   aws.Int64(MemorySize),
		Role:         l.roleArn,
		Runtime:      aws.String(Runtime),
		Timeout:      aws.Int64(Timeout),
		VpcConfig: &lambda.VpcConfig{
			SecurityGroupIds: aws.StringSlice(l.vpcConfig.SecurityGroupIds),
			SubnetIds:        aws.StringSlice(l.vpcConfig.SubnetIds),
		},
	}

	_, err = svc.CreateFunction(input)
	// Resource already exists error is fine
	if awsErr, ok := err.(awserr.Error); ok {
		if awsErr.Code() == lambda.ErrCodeResourceConflictException {
			log.Printf("Lambda function %v already exists: %v", funcName, awsErr.Message())
			return nil
		}
	}
	return AWSError(err)
}

func deleteFunction(svc LambdaAPI, functionName *string) error {
	log.Printf("Deleting the VPC connector %s", aws.StringValue(functionName))
	_, err := svc.DeleteFunction(&lambda.DeleteFunctionInput{
		FunctionName: functionName,
	})
	if err != nil {
		if functionNotExists(err) {
			return nil
		}
	}
	return AWSError(err)
}

func getFunction(svc LambdaAPI, functionName *string) (*lambda.GetFunctionOutput, error) {
	functionOutput, err := svc.GetFunction(&lambda.GetFunctionInput{FunctionName: functionName})
	if err != nil {
		return nil, err
	}
	return functionOutput, nil
}

func updateFunction(svc LambdaAPI, l *lambdaResource) error {
	log.Printf("Checking for any updates required for VPC connector %s", *l.functionName)
	zip, hash, err := getZip(l.functionFile)
	if err != nil {
		return err
	}

	if hash != *l.functionOutput.Configuration.CodeSha256 {
		log.Printf("Proceeding with code update for VPC connector %s", *l.functionName)
		codeInput := &lambda.UpdateFunctionCodeInput{
			FunctionName: l.functionName,
			ZipFile:      zip,
		}
		_, err = svc.UpdateFunctionCode(codeInput)
		if err != nil {
			return AWSError(err)
		}
	}
	configInput := &lambda.UpdateFunctionConfigurationInput{
		FunctionName: l.functionName,
		Handler:      aws.String(Handler),
		MemorySize:   aws.Int64(MemorySize),
		Role:         l.roleArn,
		Runtime:      aws.String(Runtime),
		Timeout:      aws.Int64(Timeout),
		VpcConfig: &lambda.VpcConfig{
			SecurityGroupIds: aws.StringSlice(l.vpcConfig.SecurityGroupIds),
			SubnetIds:        aws.StringSlice(l.vpcConfig.SubnetIds),
		},
	}
	_, err = svc.UpdateFunctionConfiguration(configInput)
	return AWSError(err)
}

func checklambdaState(svc LambdaAPI, functionName *string) (State, error) {
	log.Printf("Checking the state of VPC connector %s", *functionName)
	o, err := getFunction(svc, functionName)
	if err != nil {
		if functionNotExists(err) {
			return StateNotFound, nil
		} else {
			return "", AWSError(err)
		}
	}
	log.Printf("Found connector %s in %s state", *functionName, State(*o.Configuration.State))
	return State(*o.Configuration.State), nil
}

func invokeLambda(svc LambdaAPI, functionName *string, event *Event) (*LambdaResponse, error) {
	log.Printf("Invoking VPC connector %s for action: %s", *functionName, event.Action)
	eventJSON, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}
	input := &lambda.InvokeInput{
		FunctionName: functionName,
		Payload:      eventJSON,
	}

	result, err := svc.Invoke(input)
	if err != nil {
		return nil, AWSError(err)
	}
	if result.FunctionError != nil {
		log.Printf("Remote execution error: %v\n", *result.FunctionError)
		errorDetails := make(map[string]string)
		err := json.Unmarshal(result.Payload, &errorDetails)
		errMsg := ""
		if err != nil {
			log.Println(err.Error())
			errMsg = fmt.Sprintf("[%v] %v", *result.FunctionError, string(result.Payload))
		} else {
			errMsg = fmt.Sprintf("[%v] %v", errorDetails["errorType"], errorDetails["errorMessage"])
		}
		return nil, errors.New(errMsg)
	}
	resp := &LambdaResponse{}
	err = json.Unmarshal(result.Payload, resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func getZip(file string) ([]byte, string, error) {
	hasher := sha256.New()
	s, err := ioutil.ReadFile(file)
	hasher.Write(s)
	if err != nil {
		return nil, "", err
	}
	return s, base64.StdEncoding.EncodeToString(hasher.Sum(nil)), nil
}

func functionNotExists(err error) bool {
	if aerr, ok := err.(awserr.Error); ok {
		return aerr.Code() == lambda.ErrCodeResourceNotFoundException
	}
	return false
}

func newLambdaResource(svc STSAPI, cluster *string, kubeconfig *string, vpc *VPCConfiguration) *lambdaResource {
	var nameSuffix *string
	var err error
	l := &lambdaResource{
		functionFile: ZipFile,
	}
	if vpc != nil {
		suffix := fmt.Sprintf("%s-%s", strings.Join(vpc.SecurityGroupIds, "-"), strings.Join(vpc.SubnetIds, "-"))

		switch {
		case cluster != nil:
			s := fmt.Sprintf("%s-%s", *cluster, suffix)
			nameSuffix = getHash(s)
		case kubeconfig != nil:
			s := fmt.Sprintf("%s-%s", *kubeconfig, suffix)
			nameSuffix = getHash(s)
		}
		l.functionName = aws.String(FunctionNamePrefix + *nameSuffix)
		l.vpcConfig = vpc
		l.nameSuffix = nameSuffix
	}

	if svc != nil {
		l.roleArn, err = getCurrentRoleARN(svc)
		if err != nil {
			return nil
		}
	}
	return l
}
