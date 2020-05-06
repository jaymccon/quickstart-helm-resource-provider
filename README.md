# AWSQS::Kubernetes::Helm

An AWS CloudFormation resource provider for the management of helm 3 resources in EKS and self-managed Kubernetes clusters.

Properties and available attributes (ReadOnlyProperties) are documented in 
the [schema](./awsqs-kubernetes-helm.json).

## Installation
```bash
aws cloudformation create-stack \
  --stack-name awsqs-kubernetes-helm-resource \
  --capabilities CAPABILITY_NAMED_IAM \
  --template-url https://s3.amazonaws.com/aws-quickstart/quickstart-helm-resource-provider/deploy.template.yaml \
  --region us-west-2

aws cloudformation describe-stacks \
--stack-name awsqs-kubernetes-helm-resource | jq -r ".Stacks[0].Outputs[0].OutputValue" 
```
A [template](./deploy.template.yaml) is provided to make deploying the resource into 
an account easy. Use the role ARN to provide access to the cluster. In case of self-managed Kubernetes create upload yhe kubeconfig file to the AWS Secrets Manager.

Example usage:

```yaml
AWSTemplateFormatVersion: 2010-09-09
Parameters:
  Cluster:
    Type: String
Resources:
  TestResource:
    Type: AWSQS::Kubernetes::Helm
    Properties:
      Chart: stable/jenkins
      ClusterID: !Ref Cluster
      Values:
        master.serviceType: LoadBalancer
Outputs:
  Name:
    Value: !GetAtt TestResource.Name
```