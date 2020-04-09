package resource

import (
	"errors"
	"log"
	"os"

	"github.com/aws-cloudformation/cloudformation-cli-go-plugin/cfn/handler"
	"github.com/aws/aws-sdk-go/aws"
	"helm.sh/helm/v3/pkg/helmpath/xdg"
)

const callbackDelay = 30

func init() {
	os.Setenv(xdg.CacheHomeEnvVar, helmCacheHomeEnvVar)
	os.Setenv(xdg.ConfigHomeEnvVar, helmConfigHomeEnvVar)
	os.Setenv(xdg.DataHomeEnvVar, helmDataHomeEnvVar)
	os.Setenv("KUBECONFIG", kubeConfigLocalPath)
}

// Create handles the Create event from the Cloudformation service.
func Create(req handler.Request, prevModel *Model, currentModel *Model) (handler.ProgressEvent, error) {
	if req.CallbackContext["stabilizing"] == "True" {
		data, err := decodeID(currentModel.ID)
		if err != nil {
			return handler.ProgressEvent{}, err
		}
		client, err := NewClient(aws.String(data.ClusterID), aws.String(data.KubeConfig), aws.String(data.Namespace), req.Session)
		s, err := client.helmStatus(*currentModel.Name)
		if err != nil {
			return handler.ProgressEvent{}, err
		}
		switch s.status {
		case "deployed":
			r := &releaseData{
				name:      *currentModel.Name,
				namespace: s.namespace,
				chart:     s.chart,
				manifest:  s.manifest,
			}
			pending, err := client.checkPendingResources(r)
			if err != nil {
				return handler.ProgressEvent{}, errors.New("Resources didn't stabilize")
			}
			if pending {
				log.Printf("Release %s have pending resources", r.name)
				return handler.ProgressEvent{
					OperationStatus:      handler.InProgress,
					Message:              "Release in progress",
					CallbackDelaySeconds: callbackDelay,
					CallbackContext: map[string]interface{}{
						"stabilizing": string("True"),
					},
					ResourceModel: currentModel,
				}, nil
			}
			log.Printf("Release %s have no pending resources. Sending success...", r.name)
			return handler.ProgressEvent{
				OperationStatus: handler.Success,
				Message:         "Successfully installed release",
				ResourceModel:   currentModel,
			}, nil
		case "pending-install":
			return handler.ProgressEvent{
				OperationStatus:      handler.InProgress,
				Message:              "Release in progress",
				CallbackDelaySeconds: callbackDelay,
				CallbackContext: map[string]interface{}{
					"stabilizing": string("True"),
				},
				ResourceModel: currentModel,
			}, nil
		default:
			return handler.ProgressEvent{
				OperationStatus: handler.Failed,
				Message:         "Release failed",
				ResourceModel:   currentModel,
			}, nil

		}
	}

	client, err := NewClient(currentModel.ClusterID, currentModel.KubeConfig, currentModel.Namespace, req.Session)
	if err != nil {
		return handler.ProgressEvent{}, err
	}

	inputs, err := client.processInputs(currentModel)
	if err != nil {
		return handler.ProgressEvent{}, err
	}
	currentModel.Name = inputs.config.name
	currentModel.ID, err = generateID(currentModel, *inputs.config.name, aws.StringValue(req.Session.Config.Region), *inputs.config.namespace)
	err = client.helmInstall(inputs.config, inputs.valueOpts)
	if err != nil {
		return handler.ProgressEvent{}, err
	}
	response := handler.ProgressEvent{
		OperationStatus:      handler.InProgress,
		Message:              "Release in progress",
		ResourceModel:        currentModel,
		CallbackDelaySeconds: callbackDelay,
		CallbackContext: map[string]interface{}{
			"stabilizing": string("True"),
		},
	}
	return response, nil
}

// Read handles the Read event from the Cloudformation service.
func Read(req handler.Request, prevModel *Model, currentModel *Model) (handler.ProgressEvent, error) {
	data, err := decodeID(currentModel.ID)
	if err != nil {
		return handler.ProgressEvent{}, err
	}

	client, err := NewClient(aws.String(data.ClusterID), aws.String(data.KubeConfig), aws.String(data.Namespace), req.Session)
	if err != nil {
		return handler.ProgressEvent{}, err
	}
	s, err := client.helmStatus(data.Name)
	if err != nil {
		return handler.ProgressEvent{}, err
	}
	r := &releaseData{
		name:      data.Name,
		namespace: s.namespace,
		chart:     s.chart,
		manifest:  s.manifest,
	}
	res, err := client.getKubeResources(r)
	if err != nil {
		return handler.ProgressEvent{}, err
	}
	currentModel.Name = aws.String(data.Name)
	currentModel.Namespace = aws.String(data.Namespace)
	currentModel.Chart = aws.String(s.chartName)
	currentModel.Version = aws.String(s.chartVersion)
	currentModel.Resources = res
	return handler.ProgressEvent{
		OperationStatus: handler.Success,
		Message:         "Read complete",
		ResourceModel:   currentModel,
	}, nil
}

// Update handles the Update event from the Cloudformation service.
func Update(req handler.Request, prevModel *Model, currentModel *Model) (handler.ProgressEvent, error) {
	client, err := NewClient(currentModel.ClusterID, currentModel.KubeConfig, currentModel.Namespace, req.Session)
	if err != nil {
		return handler.ProgressEvent{}, err
	}
	if req.CallbackContext["stabilizing"] == "True" {
		s, err := client.helmStatus(*currentModel.Name)
		if err != nil {
			return handler.ProgressEvent{}, err
		}
		switch s.status {
		case "deployed":
			r := &releaseData{
				name:      *currentModel.Name,
				namespace: s.namespace,
				chart:     s.chart,
				manifest:  s.manifest,
			}
			pending, err := client.checkPendingResources(r)
			if err != nil {
				return handler.ProgressEvent{}, errors.New("Resources didn't stabilize")
			}
			if pending {
				log.Printf("Release %s have pending resources", r.name)
				return handler.ProgressEvent{
					OperationStatus:      handler.InProgress,
					Message:              "Release in progress",
					CallbackDelaySeconds: callbackDelay,
					CallbackContext: map[string]interface{}{
						"stabilizing": string("True"),
					},
					ResourceModel: currentModel,
				}, nil
			}
			log.Printf("Release %s have no pending resources. Sending success...", r.name)
			return handler.ProgressEvent{
				OperationStatus: handler.Success,
				Message:         "Successfully installed release",
				ResourceModel:   currentModel,
			}, nil
		case "pending-upgrade":
			return handler.ProgressEvent{
				OperationStatus:      handler.InProgress,
				Message:              "Release in progress",
				CallbackDelaySeconds: callbackDelay,
				CallbackContext: map[string]interface{}{
					"stabilizing": string("True"),
				},
				ResourceModel: currentModel,
			}, nil
		default:
			return handler.ProgressEvent{
				OperationStatus: handler.Failed,
				Message:         "Release failed",
				ResourceModel:   currentModel,
			}, nil

		}
	}
	inputs, err := client.processInputs(currentModel)
	if err != nil {
		return handler.ProgressEvent{}, err
	}
	data, err := decodeID(currentModel.ID)
	if err != nil {
		return handler.ProgressEvent{}, err
	}
	err = client.helmUpgrade(data.Name, inputs.config, inputs.valueOpts)
	if err != nil {
		return handler.ProgressEvent{}, err
	}
	currentModel.Name = aws.String(data.Name)
	response := handler.ProgressEvent{
		OperationStatus:      handler.InProgress,
		Message:              "Release in progress",
		ResourceModel:        currentModel,
		CallbackDelaySeconds: callbackDelay,
		CallbackContext: map[string]interface{}{
			"stabilizing": string("True"),
		},
	}
	return response, nil
}

// Delete handles the Delete event from the Cloudformation service.
func Delete(req handler.Request, prevModel *Model, currentModel *Model) (handler.ProgressEvent, error) {
	client, err := NewClient(currentModel.ClusterID, currentModel.KubeConfig, currentModel.Namespace, req.Session)
	if err != nil {
		return handler.ProgressEvent{}, err
	}
	data, err := decodeID(currentModel.ID)
	if err != nil {
		return handler.ProgressEvent{}, err
	}
	err = client.helmUninstall(data.Name)
	if err != nil {
		return handler.ProgressEvent{}, err
	}
	response := handler.ProgressEvent{
		OperationStatus: handler.Success,
		Message:         "Uninstall complete",
		ResourceModel:   currentModel,
	}

	return response, nil
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
