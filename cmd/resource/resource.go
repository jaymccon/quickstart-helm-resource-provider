package resource

import (
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/aws-cloudformation/cloudformation-cli-go-plugin/cfn/handler"
	"github.com/aws/aws-sdk-go/aws"
	"helm.sh/helm/v3/pkg/helmpath/xdg"
)

func init() {
	os.Setenv("HELM_DRIVER", HelmDriver)
	os.Setenv(xdg.CacheHomeEnvVar, HelmCacheHomeEnvVar)
	os.Setenv(xdg.ConfigHomeEnvVar, HelmConfigHomeEnvVar)
	os.Setenv(xdg.DataHomeEnvVar, HelmDataHomeEnvVar)
	os.Setenv("StartTime", time.Now().Format(time.RFC3339))
}

// Create handles the Create event from the CloudFormation service.
func Create(req handler.Request, _ *Model, currentModel *Model) (handler.ProgressEvent, error) {
	defer LogPanic()
	stage := getStage(req.CallbackContext)
	switch stage {
	case InitStage, LambdaStabilize:
		log.Printf("Starting %s...", stage)
		if currentModel.Name == nil {
			currentModel.Name = getReleaseNameContext(req.CallbackContext)
		}
		return initialize(req.Session, currentModel, InstallReleaseAction), nil
	case ReleaseStabilize:
		log.Printf("Starting %s...", stage)
		return checkReleaseStatus(req.Session, currentModel, CompleteStage), nil
	default:
		log.Println("Failed to identify stage.")
		return makeEvent(currentModel, NoStage, fmt.Errorf("unhandled stage %s", stage)), nil
	}
}

// Read handles the Read event from the CloudFormation service.
func Read(req handler.Request, _ *Model, currentModel *Model) (handler.ProgressEvent, error) {
	var err error
	data, err := DecodeID(currentModel.ID)
	if err != nil {
		return handler.ProgressEvent{}, err
	}
	client, err := NewClients(data.ClusterID, data.KubeConfig, data.Namespace, req.Session, currentModel.RoleArn, nil, currentModel.VPCConfiguration)
	if err != nil {
		return makeEvent(currentModel, NoStage, err), nil
	}
	if IsZero(currentModel.VPCConfiguration) && currentModel.ClusterID != nil {
		currentModel.VPCConfiguration, err = getVpcConfig(client.AWSClients.EKSClient(nil, nil), client.AWSClients.EC2Client(nil, nil), currentModel)
		if err != nil {
			return makeEvent(currentModel, NoStage, err), nil
		}
	}
	currentModel.Name = data.Name
	currentModel.Namespace = data.Namespace

	e := &Event{}
	e.Model = currentModel

	vpc := false
	if !IsZero(currentModel.VPCConfiguration) {
		vpc = true
		e.Action = GetResourcesAction
		e.Kubeconfig, err = getLocalKubeConfig()
		if err != nil {
			return makeEvent(currentModel, NoStage, err), nil
		}
		u, err := client.initializeLambda(client.LambdaResource)
		if err != nil {
			return makeEvent(currentModel, NoStage, err), nil
		}
		if !u {
			return makeEvent(currentModel, NoStage, fmt.Errorf("vpc connector didn't stabilize in time")), nil
		}
	}
	s, err := client.helmStatusWrapper(data.Name, e, client.LambdaResource.functionName, vpc)
	if err != nil {
		return makeEvent(currentModel, NoStage, err), nil
	}
	currentModel.Chart = aws.String(s.ChartName)
	currentModel.Version = aws.String(s.ChartVersion)
	/* Disable fetching resources created by helm
	e.ReleaseData = &ReleaseData{
		Name:      aws.StringValue(data.Name),
		Namespace: s.Namespace,
		Chart:     s.Chart,
		Manifest:  s.Manifest,
	}
	currentModel.Resources, err = client.kubeResourcesWrapper(e, client.LambdaResource.functionName, vpc)
	if err != nil {
		return makeEvent(currentModel, NoStage, err), nil
	}*/
	return makeEvent(currentModel, CompleteStage, nil), nil
}

// Update handles the Update event from the CloudFormation service.
func Update(req handler.Request, _ *Model, currentModel *Model) (handler.ProgressEvent, error) {
	defer LogPanic()
	stage := getStage(req.CallbackContext)
	switch stage {
	case InitStage, LambdaStabilize:
		log.Printf("Starting %s...", stage)
		if currentModel.Name == nil {
			currentModel.Name = getReleaseNameContext(req.CallbackContext)
		}
		return initialize(req.Session, currentModel, UpdateReleaseAction), nil
	case ReleaseStabilize:
		log.Printf("Starting %s...", stage)
		return checkReleaseStatus(req.Session, currentModel, CompleteStage), nil
	default:
		log.Println("Failed to identify stage.")
		return makeEvent(currentModel, NoStage, fmt.Errorf("unhandled stage %s", stage)), nil
	}
}

// Delete handles the Delete event from the CloudFormation service.
func Delete(req handler.Request, _ *Model, currentModel *Model) (handler.ProgressEvent, error) {
	defer LogPanic()
	stage := getStage(req.CallbackContext)
	switch stage {
	case InitStage, LambdaStabilize, UninstallRelease, ReleaseStabilize:
		log.Printf("Starting %s...", stage)
		return initialize(req.Session, currentModel, UninstallReleaseAction), nil
	default:
		log.Println("Failed to identify stage.")
		return makeEvent(currentModel, NoStage, fmt.Errorf("unhandled stage %s", stage)), nil
	}
}

// List handles the List event from the CloudFormation service.
func List(req handler.Request, _ *Model, currentModel *Model) (handler.ProgressEvent, error) {
	// Add your code here:
	// * Make API calls (use req.Session)
	// * Mutate the model
	// * Check/set any callback context (req.CallbackContext / response.CallbackContext)

	/*
	   // Construct a new handler.ProgressEvent and return it
	   response := handler.ProgressEvent{
	       OperationStatus: handler.Success,
	       Message: "List complete",
	       ResourceModel: currentModel,
	   }

	   return response, nil
	*/

	// Not implemented, return an empty handler.ProgressEvent
	// and an error
	return handler.ProgressEvent{}, errors.New("not implemented: List")
}
