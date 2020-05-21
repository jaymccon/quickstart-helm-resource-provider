package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws-quickstart/quickstart-helm-resource-provider/cmd/resource"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/aws"
)

func HandleRequest(_ context.Context, e resource.Event) (*resource.LambdaResponse, error) {
	defer resource.LogPanic()

	res := &resource.LambdaResponse{}
	eJson, err := json.Marshal(e)
	if err != nil {
		fmt.Println(err)
	}
	fmt.Println(string(eJson))
	data, err := resource.DecodeID(e.Model.ID)
	if err != nil {
		return nil, err
	}
	client, err := resource.NewClients(nil, nil, aws.String(data.Namespace), nil, nil, e.Kubeconfig)

	switch e.Action {
	case resource.InstallReleaseAction:
		fmt.Println("InstallReleaseAction")
		return nil, client.HelmInstall(e.Inputs.Config, e.Inputs.ValueOpts, e.Inputs.ChartDetails)
	case resource.CheckReleaseAction:
		fmt.Println("CheckReleaseAction")
		res.StatusData, err = client.HelmStatus(data.Name)
		return res, err
	case resource.GetPendingAction:
		fmt.Println("GetPendingAction")
		res.PendingResources, err = client.CheckPendingResources(e.ReleaseData)
		return res, err
	case resource.GetResourcesAction:
		fmt.Println("GetResourcesAction")
		res.Resources, err = client.GetKubeResources(e.ReleaseData)
		return res, err
	case resource.UpdateReleaseAction:
		fmt.Println("UpdateReleaseAction")
		return nil, client.HelmUpgrade(data.Name, e.Inputs.Config, e.Inputs.ValueOpts, e.Inputs.ChartDetails)
	case resource.UninstallReleaseAction:
		fmt.Println("UninstallReleaseAction")
		return nil, client.HelmUninstall(data.Name)
	case resource.ListReleaseAction:
		fmt.Println("ListReleaseAction")
		res.ListData, err = client.HelmList(e.Inputs.Config, e.Inputs.ChartDetails)
		return res, err
	default:
		return nil, fmt.Errorf("Unhandled stage %s", e.Action)
	}
}

func main() {
	lambda.Start(HandleRequest)
}
