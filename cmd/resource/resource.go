package resource

import (
	"errors"
	"fmt"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ec2/ec2iface"
	eks2 "github.com/aws/aws-sdk-go/service/eks"
	"log"
	"os"
	"time"

	"github.com/aws-cloudformation/cloudformation-cli-go-plugin/cfn/handler"
	"github.com/aws/aws-sdk-go/aws"
	"helm.sh/helm/v3/pkg/helmpath/xdg"
)

const callbackDelay = 30

func init() {
	os.Setenv("HELM_DRIVER", HelmDriver)
	os.Setenv(xdg.CacheHomeEnvVar, HelmCacheHomeEnvVar)
	os.Setenv(xdg.ConfigHomeEnvVar, HelmConfigHomeEnvVar)
	os.Setenv(xdg.DataHomeEnvVar, HelmDataHomeEnvVar)
	os.Setenv("StartTime", time.Now().Format(time.RFC3339))
}

// Create handles the Create event from the Cloudformation service.
func Create(req handler.Request, prevModel *Model, currentModel *Model) (handler.ProgressEvent, error) {
	defer LogPanic()
	if currentModel.VPCConfiguration == nil {
		err := getVpcConfig(req.Session, currentModel)
		if err != nil {
			return makeEvent(currentModel, NoStage, err), nil
		}
	}
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
		return makeEvent(currentModel, NoStage, fmt.Errorf("Unhandled stage %s", stage)), nil
	}
}

func getVpcConfig(session *session.Session, model *Model) error {

	if model.ClusterID == nil || model.VPCConfiguration != nil {
		return nil
	}
	client, err := NewClients(model.ClusterID, model.KubeConfig, model.Namespace, session, model.RoleArn, nil)
	if err != nil {
		return err
	}
	eks := client.EKSClient(nil, nil)
	resp, err := eks.DescribeCluster(&eks2.DescribeClusterInput{Name: model.ClusterID})
	if err != nil {
		return err
	}
	if *resp.Cluster.ResourcesVpcConfig.EndpointPublicAccess == true && resp.Cluster.ResourcesVpcConfig.PublicAccessCidrs[0] == aws.String("0.0.0.0/0") {
		return nil
	}
	log.Println("Detected private cluster, adding VPC Configuration...")
	subnets, err := filterNattedSubnets(client.EC2Client(nil, nil), resp.Cluster.ResourcesVpcConfig.SubnetIds)
	if err != nil {
		return err
	}
	model.VPCConfiguration = &VPCConfiguration{
		SecurityGroupIds: aws.StringValueSlice(resp.Cluster.ResourcesVpcConfig.SecurityGroupIds),
		SubnetIds:        aws.StringValueSlice(subnets),
	}
	return nil
}

func filterNattedSubnets(ec2client ec2iface.EC2API, subnets []*string) (filtered []*string, err error) {
	resp, err := ec2client.DescribeSubnets(&ec2.DescribeSubnetsInput{
		SubnetIds: subnets,
	})
	if err != nil {
		return filtered, err
	}
	for _, subnet := range resp.Subnets {
		resp, err := ec2client.DescribeRouteTables(&ec2.DescribeRouteTablesInput{
			Filters: []*ec2.Filter{
				{
					Name:   aws.String("association.subnet-id"),
					Values: []*string{subnet.SubnetId},
				},
				{
					Name:   aws.String("vpc-id"),
					Values: []*string{subnet.VpcId},
				},
			},
		})
		if err != nil {
			return filtered, err
		}
		for _, route := range resp.RouteTables[0].Routes {
			if route.NatGatewayId != nil {
				filtered = append(filtered, subnet.SubnetId)
			}
		}
	}
	return filtered, err
}

// Read handles the Read event from the Cloudformation service.
func Read(req handler.Request, prevModel *Model, currentModel *Model) (handler.ProgressEvent, error) {
	if currentModel.VPCConfiguration == nil {
		err := getVpcConfig(req.Session, currentModel)
		if err != nil {
			return makeEvent(currentModel, NoStage, err), nil
		}
	}
	data, err := DecodeID(currentModel.ID)
	if err != nil {
		return handler.ProgressEvent{}, err
	}
	client, err := NewClients(aws.String(data.ClusterID), aws.String(data.KubeConfig), aws.String(data.Namespace), req.Session, currentModel.RoleArn, nil)
	if err != nil {
		return makeEvent(currentModel, NoStage, err), nil
	}
	s, err := client.HelmStatus(data.Name)
	if err != nil {
		return makeEvent(currentModel, NoStage, err), nil
	}
	currentModel.Name = aws.String(data.Name)
	currentModel.Namespace = aws.String(data.Namespace)
	currentModel.Chart = aws.String(s.ChartName)
	currentModel.Version = aws.String(s.ChartVersion)
	e := &Event{}
	e.Model = currentModel
	e.ReleaseData = &ReleaseData{
		Name:      data.Name,
		Namespace: s.Namespace,
		Chart:     s.Chart,
		Manifest:  s.Manifest,
	}
	l := newLambdaResource(client.STSClient(nil, nil), currentModel.ClusterID, currentModel.KubeConfig, currentModel.VPCConfiguration)

	vpc := false
	if currentModel.VPCConfiguration != nil {
		vpc = true
		e.Action = GetResourcesAction
		e.Kubeconfig, err = getLocalKubeConfig()
		if err != nil {
			return makeEvent(currentModel, NoStage, err), nil
		}
		u, err := client.initializeLambda(l)
		if err != nil {
			return makeEvent(currentModel, NoStage, err), nil
		}
		if !u {
			return makeEvent(currentModel, NoStage, fmt.Errorf("vpc connector didn't stabilize in time")), nil
		}
	}

	currentModel.Resources, err = client.kubeResourcesWrapper(&data.Name, e, l.functionName, vpc)
	if err != nil {
		return makeEvent(currentModel, NoStage, err), nil
	}
	return makeEvent(currentModel, CompleteStage, nil), nil
}

// Update handles the Update event from the Cloudformation service.
func Update(req handler.Request, prevModel *Model, currentModel *Model) (handler.ProgressEvent, error) {
	defer LogPanic()
	if currentModel.VPCConfiguration == nil {
		err := getVpcConfig(req.Session, currentModel)
		if err != nil {
			return makeEvent(currentModel, NoStage, err), nil
		}
	}
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
		return makeEvent(currentModel, NoStage, fmt.Errorf("Unhandled stage %s", stage)), nil
	}
}

// Delete handles the Delete event from the Cloudformation service.
func Delete(req handler.Request, prevModel *Model, currentModel *Model) (handler.ProgressEvent, error) {
	defer LogPanic()
	if currentModel.VPCConfiguration == nil {
		err := getVpcConfig(req.Session, currentModel)
		if err != nil {
			return makeEvent(currentModel, NoStage, err), nil
		}
	}
	stage := getStage(req.CallbackContext)
	switch stage {
	case InitStage, LambdaStabilize, ReleaseDelete, ReleaseStabilize:
		log.Printf("Starting %s...", stage)
		return initialize(req.Session, currentModel, UninstallReleaseAction), nil
	default:
		log.Println("Failed to identify stage.")
		return makeEvent(currentModel, NoStage, fmt.Errorf("Unhandled stage %s", stage)), nil
	}
}

// List handles the List event from the Cloudformation service.
func List(req handler.Request, prevModel *Model, currentModel *Model) (handler.ProgressEvent, error) {
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
	return handler.ProgressEvent{}, errors.New("Not implemented: List")
}
