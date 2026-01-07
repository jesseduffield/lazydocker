package entities

var (
	TypePVC     = "PersistentVolumeClaim"
	TypePod     = "Pod"
	TypeService = "Service"
)

// ApplyOptions controls the deployment of kube yaml files to a Kubernetes Cluster
type ApplyOptions struct {
	// Kubeconfig - path to the cluster's kubeconfig file.
	Kubeconfig string
	// Namespace - namespace to deploy the workload in on the cluster.
	Namespace string
	// CACertFile - the path to the CA cert file for the Kubernetes cluster.
	CACertFile string
	// File - the path to the Kubernetes yaml to deploy.
	File string
	// Service - creates a service for the container being deployed.
	Service bool
}
