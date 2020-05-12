# AWSQS::Kubernetes::Helm

A resource provider for managing helm.

## Syntax

To declare this entity in your AWS CloudFormation template, use the following syntax:

### JSON

<pre>
{
    "Type" : "AWSQS::Kubernetes::Helm",
    "Properties" : {
        "<a href="#clusterid" title="ClusterID">ClusterID</a>" : <i>String</i>,
        "<a href="#kubeconfig" title="KubeConfig">KubeConfig</a>" : <i>String</i>,
        "<a href="#rolearn" title="RoleArn">RoleArn</a>" : <i>String</i>,
        "<a href="#repository" title="Repository">Repository</a>" : <i>String</i>,
        "<a href="#values" title="Values">Values</a>" : <i><a href="values.md">Values</a></i>,
        "<a href="#valueyaml" title="ValueYaml">ValueYaml</a>" : <i>String</i>,
        "<a href="#valueoverrideurl" title="ValueOverrideURL">ValueOverrideURL</a>" : <i>String</i>,
        "<a href="#id" title="ID">ID</a>" : <i>String</i>,
        "<a href="#timeout" title="TimeOut">TimeOut</a>" : <i>Double</i>,
        "<a href="#vpcconfiguration" title="VPCConfiguration">VPCConfiguration</a>" : <i><a href="vpcconfiguration.md">VPCConfiguration</a></i>
    }
}
</pre>

### YAML

<pre>
Type: AWSQS::Kubernetes::Helm
Properties:
    <a href="#clusterid" title="ClusterID">ClusterID</a>: <i>String</i>
    <a href="#kubeconfig" title="KubeConfig">KubeConfig</a>: <i>String</i>
    <a href="#rolearn" title="RoleArn">RoleArn</a>: <i>String</i>
    <a href="#repository" title="Repository">Repository</a>: <i>String</i>
    <a href="#values" title="Values">Values</a>: <i><a href="values.md">Values</a></i>
    <a href="#valueyaml" title="ValueYaml">ValueYaml</a>: <i>String</i>
    <a href="#valueoverrideurl" title="ValueOverrideURL">ValueOverrideURL</a>: <i>String</i>
    <a href="#id" title="ID">ID</a>: <i>String</i>
    <a href="#timeout" title="TimeOut">TimeOut</a>: <i>Double</i>
    <a href="#vpcconfiguration" title="VPCConfiguration">VPCConfiguration</a>: <i><a href="vpcconfiguration.md">VPCConfiguration</a></i>
</pre>

## Properties

#### ClusterID

EKS cluster name

_Required_: No

_Type_: String

_Update requires_: [Replacement](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/using-cfn-updating-stacks-update-behaviors.html#update-replacement)

#### KubeConfig

_Required_: No

_Type_: String

_Pattern_: <code>^arn:aws(-(cn|gov))?:[a-z-]+:(([a-z]+-)+[0-9])?:([0-9]{12})?:[^.]+$</code>

_Update requires_: [No interruption](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/using-cfn-updating-stacks-update-behaviors.html#update-no-interrupt)

#### RoleArn

_Required_: No

_Type_: String

_Pattern_: <code>^arn:aws(-(cn|gov))?:[a-z-]+:(([a-z]+-)+[0-9])?:([0-9]{12})?:[^.]+$</code>

_Update requires_: [No interruption](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/using-cfn-updating-stacks-update-behaviors.html#update-no-interrupt)

#### Repository

Repository url. Defaults to kubernetes-charts.storage.googleapis.com

_Required_: No

_Type_: String

_Update requires_: [No interruption](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/using-cfn-updating-stacks-update-behaviors.html#update-no-interrupt)

#### Values

Custom Values can optionally be specified

_Required_: No

_Type_: <a href="values.md">Values</a>

_Update requires_: [No interruption](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/using-cfn-updating-stacks-update-behaviors.html#update-no-interrupt)

#### ValueYaml

String representation of a values.yaml file

_Required_: No

_Type_: String

_Update requires_: [No interruption](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/using-cfn-updating-stacks-update-behaviors.html#update-no-interrupt)

#### ValueOverrideURL

Custom Value Yaml file can optionally be specified

_Required_: No

_Type_: String

_Pattern_: <code>^[sS]3://[0-9a-zA-Z]([-.\w]*[0-9a-zA-Z])(:[0-9]*)*([?/#].*)?$</code>

_Update requires_: [No interruption](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/using-cfn-updating-stacks-update-behaviors.html#update-no-interrupt)

#### ID

Primary identifier for Cloudformation

_Required_: No

_Type_: String

_Update requires_: [No interruption](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/using-cfn-updating-stacks-update-behaviors.html#update-no-interrupt)

#### TimeOut

Timeout for resource provider. Default 60 mins

_Required_: No

_Type_: Double

_Update requires_: [No interruption](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/using-cfn-updating-stacks-update-behaviors.html#update-no-interrupt)

#### VPCConfiguration

For network connectivity to Cluster inside VPC

_Required_: No

_Type_: <a href="vpcconfiguration.md">VPCConfiguration</a>

_Update requires_: [No interruption](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/using-cfn-updating-stacks-update-behaviors.html#update-no-interrupt)

## Return Values

### Ref

When you pass the logical ID of this resource to the intrinsic `Ref` function, Ref returns the ID.

### Fn::GetAtt

The `Fn::GetAtt` intrinsic function returns a value for a specified attribute of this type. The following are the available attributes and sample return values.

For more information about using the `Fn::GetAtt` intrinsic function, see [Fn::GetAtt](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/intrinsic-function-reference-getatt.html).

#### Name

Name for the helm release

#### Namespace

Namespace to use with helm. Created if doesn't exist and default will be used if not provided

#### Chart

Chart name

#### Version

Version can be specified, if not latest will be used

#### Resources

Resources from the helm charts

