

# Description
The GitLab Runner Operator can be used to manage the deployment and lifecycle of GitLab Runners on Kubernetes. It helps streamline the installation and simplify the configuration, especially when used in conjunction with config management solutions such as Anthos Config Management.

# Standard Installation

## Deploy the Operator

```shell
$ kubectl apply -f https://raw.githubusercontent.com/matthewmarr/devops/main/gitlab-runner-operator/kubernetes/acm/cluster/gitlab-runner-operator.yaml
```

## Configure custom resource and deploy to the cluster

#### Create the namespace that the GitLab runner will be deployed into.
```shell
$ export NAMESPACE=gitlab
$ kubectl create namespace ${NAMESPACE}
```

#### The following options can be configured depending on how the runner should function.
| Field | Description | Required |
| ----- | ----------- | -------- |
| maxConcurrentJobs | The [concurrent](https://docs.gitlab.com/runner/configuration/advanced-configuration.html#the-global-section) jobs that can run at one time | No |
| credentials.tokenSecretName | The name of the secret that will contain the GitLab runner [token](https://docs.gitlab.com/ee/ci/runners/#configuring-runners-in-gitlab). | Yes |
| runnerServiceAccount.name | The name of the k8s service account to create for the runner. | Yes |
| runnerServiceAccount.imagePullSecretName | The name of the secret containing [docker registry credentials](https://kubernetes.io/docs/tasks/configure-pod-container/pull-image-private-registry/#create-a-secret-by-providing-credentials-on-the-command-line) | No |
| runnerServiceAccount.workloadIdentityServiceAccount | The name of the GCP IAM service account to use for [Workload Identity](https://cloud.google.com/kubernetes-engine/docs/how-to/workload-identity) | No |
| tagList | List of GitLab runner [tags](https://docs.gitlab.com/ee/ci/yaml/#tags) to use | Yes |
| url | Url of the GitLab instance | Yes |
| jobNamespace | The namespace to run the GitLab runner jobs in | Yes |


#### Modify the following and deploy the runner custom resource.

```shell
$ cat <<EOF | kubectl apply -f -
apiVersion: runner.gscout.xyz/v1alpha1
kind: Runner
metadata:
  name: runner-sample
  namespace: ${NAMESPACE}
spec:
  maxConcurrentJobs: 10
  credentials:
    tokenSecretName: gitlab-runner-token
    cacertSecretName: gitlab-ca-cert
  runnerServiceAccount:
    name: gitlab-runner
    #imagePullSecretName: registry-credentials
    #workloadIdentityServiceAccount: gitlab-wi@example-project.iam.gserviceaccount.com
  tagList:
  - sample_tag_1
  - sample_tag_2
  url: https://gitlab.com
  jobNamespace: ${NAMESPACE}
EOF
```
<br>

#### Check the status of the runner

Once the runner resource is deployed, you can view the status. If you haven't created the required secret(s), you will notice the runner deployment is pending.

```shell
$ kubectl get runners -A
NAMESPACE   NAME            URL                  TAGS                  REGISTERED    STATUS
gitlab      runner-sample   https://gitlab.com   [sample_tag_1...]     false         Pending Secret Creation
```

## Create secret(s)

In order for the operator create the required resources for the GitLab runner, you will need to create a secret containing the GitLab runner registration token. If you specified a `imagePullSecretName` or `cacertSecretName`, you will also need to create secrets for those values.
<br><br>

#### <strong>Required</strong> - Create GitLab Registration Token Secret
This secret will contain the GitLab runner registration token supplied by your GitLab instance. The secret key needs to be set as `runner-registration-token`.

```shell
$ export TOKEN=<GITLAB_REGISTRATION_TOKEN>
$ kubectl create secret generic gitlab-runner-token -n ${NAMESPACE} --from-literal=runner-registration-token=${TOKEN}
```

#### <strong>Optional</strong> - Create GitLab server `ca-cert` secret
If `cacertSecretName` was specified, you will need to create a secret containing the ca cert.

```shell
$ export CA_CERT_PATH=/home/mycerts/my_cert.crt
$ export CA_CERT_HOSTNAME=my.gitlab.server.com
$ kubectl create secret generic gitlab-ca-cert -n ${NAMESPACE} --from-file=${CA_CERT_HOSTNAME}=$(cat CA_CERT_PATH)
```

#### <strong>Optional</strong> - Create registry `imagePullSecrets` secret
If `imagePullSecretName` was specified, you will need to create a secret containing the image pull secret credentials. If using Google Container Registry and are required to provide image pull secrets (such as using an On-Prem Anthos Cluster), the following steps can be followed.

1. Create a service account that has read access to Google Container Registry images

2. Download a json key for the created service account

3. Create the secret

    ```shell
    $ export SECRET_NAME=<secret_name>
    $ export JSON_KEY=<service_account_key_path>

    $ kubectl create secret docker-registry $SECRETNAME \
      --docker-server=https://gcr.io \
      --docker-username=_json_key \
      --docker-email=user@example.com \
      --docker-password="$(cat $JSON_KEY)"
    ```

### Sample Deployment

Once you have deployed the runner custom resource, you can view the runner status. This example shows two deployed runners, one in the `gitlab` namespace, and one in the `gitlab1` namespace.

```shell
$ kubectl get runners -A
NAMESPACE   NAME            URL                  TAGS                   REGISTERED   STATUS
gitlab      runner-sample   https://gitlab.com   [prod gke-us-east]     true         Running
gitlab1     runner-sample   https://gitlab.com   [prod1 gke-us-east1]   true         Running
```



## Specify in documentation ##
* No Privileged Containers


## TODO ##
* Automatic Tag based on ACM
* GCS for cache - https://docs.gitlab.com/runner/install/kubernetes.html#google-cloud-storage-gcs