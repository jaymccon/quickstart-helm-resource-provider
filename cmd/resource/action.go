package resource

import (
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/aws-cloudformation/cloudformation-cli-go-plugin/cfn/handler"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"helm.sh/helm/v3/pkg/release"
)

type Stage string

const (
	InitStage        Stage = "Init"
	LambdaInitStage  Stage = "LambdaInit"
	ReleaseStabilize Stage = "ReleaseStabilize"
	UninstallRelease Stage = "UninstallRelease"
	LambdaStabilize  Stage = "LambdaStabilize"
	CompleteStage    Stage = "Complete"
	NoStage          Stage = "NoStage"
)

const (
	lambdaStableTryCount = 3
)

func initialize(session *session.Session, currentModel *Model, action Action) handler.ProgressEvent {
	vpc := false
	var err error
	client, err := NewClients(currentModel.ClusterID, currentModel.KubeConfig, currentModel.Namespace, session, currentModel.RoleArn, nil)
	if err != nil {
		return makeEvent(currentModel, NoStage, err)
	}
	l := newLambdaResource(client.AWSClients.STSClient(nil, nil), currentModel.ClusterID, currentModel.KubeConfig, currentModel.VPCConfiguration)
	e := &Event{}
	e.Inputs = new(Inputs)
	e.Inputs.Config = new(Config)
	e.Action = action
	e.Model = currentModel
	e.Inputs.ChartDetails, err = getChartDetails(currentModel)
	if err != nil {
		return makeEvent(currentModel, NoStage, err)
	}
	e.Inputs.Config.Name = getReleaseName(currentModel.Name, e.Inputs.ChartDetails.ChartName)
	currentModel.Name = e.Inputs.Config.Name
	e.Inputs.Config.Namespace = getReleaseNameSpace(currentModel.Namespace)
	if currentModel.ID == nil {
		currentModel.ID, err = generateID(currentModel, *e.Inputs.Config.Name, aws.StringValue(session.Config.Region), *e.Inputs.Config.Namespace)
		if err != nil {
			return makeEvent(currentModel, NoStage, err)
		}
	}
	if !IsZero(currentModel.VPCConfiguration) {
		vpc = true
		e.Kubeconfig, err = getLocalKubeConfig()
		if err != nil {
			return makeEvent(currentModel, NoStage, err)
		}
		u, err := client.initializeLambda(l)
		if err != nil {
			return makeEvent(currentModel, NoStage, err)
		}
		if !u {
			return makeEvent(currentModel, LambdaStabilize, nil)
		}
	}
	switch e.Action {
	case InstallReleaseAction:
		e.Inputs.ValueOpts, err = client.processValues(currentModel)
		if err != nil {
			return makeEvent(currentModel, NoStage, err)
		}
		data, err := DecodeID(currentModel.ID)
		if err != nil {
			return makeEvent(currentModel, NoStage, err)
		}
		currentModel.Name = aws.String(data.Name)
		e.Model = currentModel
		err = client.helmInstallWrapper(e, l.functionName, vpc)
		if err != nil {
			return makeEvent(currentModel, NoStage, err)
		}
		return makeEvent(currentModel, ReleaseStabilize, nil)
	case UpdateReleaseAction:
		e.Inputs.ValueOpts, err = client.processValues(currentModel)
		if err != nil {
			return makeEvent(currentModel, NoStage, err)
		}
		data, err := DecodeID(currentModel.ID)
		if err != nil {
			return makeEvent(currentModel, NoStage, err)
		}
		err = client.helmUpgradeWrapper(aws.String(data.Name), e, l.functionName, vpc)
		if err != nil {
			return makeEvent(currentModel, NoStage, err)
		}
		currentModel.Name = aws.String(data.Name)
		return makeEvent(currentModel, ReleaseStabilize, nil)
	case UninstallReleaseAction:
		data, err := DecodeID(currentModel.ID)
		if err != nil {
			return makeEvent(currentModel, NoStage, err)
		}
		err = client.helmDeleteWrapper(aws.String(data.Name), e, l.functionName, vpc)
		if err != nil {
			return makeEvent(currentModel, NoStage, err)
		}
		return client.lambdaDestroy(currentModel)
	}
	return makeEvent(currentModel, NoStage, fmt.Errorf("Unhandled stage %s", action))
}

func checkReleaseStatus(session *session.Session, currentModel *Model, successStage Stage) handler.ProgressEvent {
	vpc := false
	var err error
	client, err := NewClients(currentModel.ClusterID, currentModel.KubeConfig, currentModel.Namespace, session, currentModel.RoleArn, nil)
	if err != nil {
		return makeEvent(currentModel, NoStage, err)
	}
	l := newLambdaResource(client.AWSClients.STSClient(nil, nil), currentModel.ClusterID, currentModel.KubeConfig, currentModel.VPCConfiguration)
	e := &Event{}
	e.Model = currentModel
	if !IsZero(currentModel.VPCConfiguration) {
		vpc = true
		e.Kubeconfig, err = getLocalKubeConfig()
		if err != nil {
			return makeEvent(currentModel, NoStage, err)
		}
		u, err := client.initializeLambda(l)
		if err != nil {
			return makeEvent(currentModel, NoStage, err)
		}
		if !u {
			return makeEvent(currentModel, LambdaStabilize, nil)
		}
	}
	e.Action = CheckReleaseAction
	s, err := client.helmStatusWrapper(currentModel.Name, e, l.functionName, vpc)
	if err != nil {
		return makeEvent(currentModel, NoStage, err)
	}
	switch s.Status {
	case release.StatusDeployed:
		e.ReleaseData = &ReleaseData{
			Name:      *currentModel.Name,
			Namespace: s.Namespace,
			Chart:     s.Chart,
			Manifest:  s.Manifest,
		}
		e.Action = GetPendingAction
		pending, err := client.kubePendingWrapper(nil, e, l.functionName, vpc)
		if err != nil {
			return makeEvent(currentModel, NoStage, err)
		}
		if pending {
			log.Printf("Release %s have pending resources", e.ReleaseData.Name)
			return makeEvent(currentModel, ReleaseStabilize, nil)
		}
		log.Printf("Release %s have no pending resources.", e.ReleaseData.Name)
		return makeEvent(currentModel, successStage, nil)
	case release.StatusPendingInstall, release.StatusPendingUpgrade:
		return makeEvent(currentModel, ReleaseStabilize, nil)
	default:
		return makeEvent(currentModel, NoStage, errors.New("Release failed"))

	}
}

func (c *Clients) lambdaDestroy(currentModel *Model) handler.ProgressEvent {
	if IsZero(currentModel.VPCConfiguration) {
		return makeEvent(currentModel, CompleteStage, nil)
	}
	l := newLambdaResource(nil, currentModel.ClusterID, currentModel.KubeConfig, currentModel.VPCConfiguration)
	err := deleteFunction(c.AWSClients.LambdaClient(nil, nil), l.functionName)
	if err != nil {
		return makeEvent(currentModel, NoStage, err)
	}
	return makeEvent(currentModel, CompleteStage, nil)
}

func (c *Clients) initializeLambda(l *lambdaResource) (bool, error) {
	state, err := checklambdaState(c.AWSClients.LambdaClient(nil, nil), l.functionName)
	if err != nil {
		return false, err
	}
	switch state {
	case StateNotFound:
		log.Printf("VPC connector %s not found", *l.functionName)
		err := createFunction(c.AWSClients.LambdaClient(nil, nil), l)
		if err != nil {
			return false, err
		}
		count := 0
		for count < lambdaStableTryCount {
			state, err = checklambdaState(c.AWSClients.LambdaClient(nil, nil), l.functionName)
			if err != nil {
				return false, err
			}
			if state == StateActive {
				return true, nil
			}
			time.Sleep(5 * time.Second)
			count++
		}
		return false, nil
	case StateActive:
		var err error
		l.functionOutput, err = getFunction(c.AWSClients.LambdaClient(nil, nil), l.functionName)
		if err != nil {
			return false, err
		}
		err = updateFunction(c.AWSClients.LambdaClient(nil, nil), l)
		if err != nil {
			return false, err
		}
		return true, nil
	case StatePending:
		count := 0
		for count < lambdaStableTryCount {
			state, err = checklambdaState(c.AWSClients.LambdaClient(nil, nil), l.functionName)
			if err != nil {
				return false, err
			}
			if state == StateActive {
				return true, nil
			}
			time.Sleep(8 * time.Second)
			count++
		}
		return false, nil
	default:
		return false, fmt.Errorf("%s not in desired state: %s", *l.functionName, state)
	}
}

func (c *Clients) helmStatusWrapper(name *string, e *Event, functionName *string, vpc bool) (*HelmStatusData, error) {
	switch vpc {
	case true:
		r, err := invokeLambda(c.AWSClients.LambdaClient(nil, nil), functionName, e)
		if err != nil {
			return nil, err
		}
		return r.StatusData, err

	default:
		return c.HelmStatus(*name)
	}
}

func (c *Clients) helmListWrapper(name *string, e *Event, functionName *string, vpc bool) ([]HelmListData, error) {
	switch vpc {
	case true:
		r, err := invokeLambda(c.AWSClients.LambdaClient(nil, nil), functionName, e)
		if err != nil {
			return nil, err
		}
		return r.ListData, err
	default:
		return c.HelmList(e.Inputs.Config, e.Inputs.ChartDetails)
	}
}

func (c *Clients) helmInstallWrapper(e *Event, functionName *string, vpc bool) error {
	switch vpc {
	case true:
		_, err := invokeLambda(c.AWSClients.LambdaClient(nil, nil), functionName, e)
		return err
	default:
		return c.HelmInstall(e.Inputs.Config, e.Inputs.ValueOpts, e.Inputs.ChartDetails)
	}
}

func (c *Clients) helmUpgradeWrapper(name *string, e *Event, functionName *string, vpc bool) error {
	switch vpc {
	case true:
		_, err := invokeLambda(c.AWSClients.LambdaClient(nil, nil), functionName, e)
		return err
	default:
		return c.HelmUpgrade(*name, e.Inputs.Config, e.Inputs.ValueOpts, e.Inputs.ChartDetails)
	}
}

func (c *Clients) helmDeleteWrapper(name *string, e *Event, functionName *string, vpc bool) error {
	switch vpc {
	case true:
		_, err := invokeLambda(c.AWSClients.LambdaClient(nil, nil), functionName, e)
		return err
	default:
		return c.HelmUninstall(*name)
	}
}

func (c *Clients) kubePendingWrapper(name *string, e *Event, functionName *string, vpc bool) (bool, error) {
	switch vpc {
	case true:
		r, err := invokeLambda(c.AWSClients.LambdaClient(nil, nil), functionName, e)
		if err != nil {
			return true, err
		}
		return r.PendingResources, err
	default:
		return c.CheckPendingResources(e.ReleaseData)
	}
}

func (c *Clients) kubeResourcesWrapper(name *string, e *Event, functionName *string, vpc bool) (map[string]interface{}, error) {
	switch vpc {
	case true:
		r, err := invokeLambda(c.AWSClients.LambdaClient(nil, nil), functionName, e)
		if err != nil {
			return nil, err
		}
		return r.Resources, err
	default:
		return c.GetKubeResources(e.ReleaseData)
	}
}
