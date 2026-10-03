// Package kubernetes generates Kubernetes manifests for deployable
// challenges and applies them via kubectl.
//
// Only the API shapes actually used for Deployment / Service / Ingress
// are defined here.
package kubernetes

// TypeMeta holds the Kubernetes apiVersion and kind fields
type TypeMeta struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
}

// ObjectMeta the standard Kubernetes resource metadata
type ObjectMeta struct {
	Name        string            `json:"name"`
	Namespace   string            `json:"namespace,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

// ResourceList maps resource names to quantities
type ResourceList map[string]string

// ResourceRequirements holds container resource limits and requests
type ResourceRequirements struct {
	Limits   ResourceList `json:"limits,omitempty"`   // max resources the container is allowed to consume
	Requests ResourceList `json:"requests,omitempty"` // min resources the container needs to run
}

// ContainerPort exposes one port from a container
type ContainerPort struct {
	ContainerPort int    `json:"containerPort"`
	Protocol      string `json:"protocol,omitempty"`
}

// ExecAction runs a command inside the container
type ExecAction struct {
	Command []string `json:"command"`
}

// HTTPGetAction performs an HTTP GET for a probe generally used for web
// challenges to check if the website is up.
type HTTPGetAction struct {
	Path string `json:"path"`
	Port int    `json:"port"`
}

// TCPSocketAction performs a TCP socket check for services that don't serve HTTP.
type TCPSocketAction struct {
	Port int `json:"port"`
}

// Probe defines a liveness, readiness, or startup probe:
//   - Exec: file system checks or running a script inside the container
//   - HTTPGet: web challenges to check if the website is up
//   - TCPSocket: TCP challenges to check if the socket is accepting connections
//   - InitialDelaySeconds: how long to wait before first probe
//   - PeriodSeconds: how often to check
//   - TimeoutSeconds: how long to wait for a response
//   - FailureThreshold: how many times to check before giving up
type Probe struct {
	Exec                *ExecAction      `json:"exec,omitempty"`
	HTTPGet             *HTTPGetAction   `json:"httpGet,omitempty"`
	TCPSocket           *TCPSocketAction `json:"tcpSocket,omitempty"`
	InitialDelaySeconds int              `json:"initialDelaySeconds,omitempty"`
	PeriodSeconds       int              `json:"periodSeconds,omitempty"`
	TimeoutSeconds      int              `json:"timeoutSeconds,omitempty"`
	FailureThreshold    int              `json:"failureThreshold,omitempty"`
}

// Capabilities lists Linux capabilities to add or drop
type Capabilities struct {
	Add  []string `json:"add,omitempty"`
	Drop []string `json:"drop,omitempty"`
}

// SeccompProfile defines the seccomp profile applied to a container
type SeccompProfile struct {
	Type string `json:"type"`
}

// SecurityContext holds container-level security options
type SecurityContext struct {
	AllowPrivilegeEscalation *bool           `json:"allowPrivilegeEscalation,omitempty"`
	Privileged               *bool           `json:"privileged,omitempty"`
	Capabilities             *Capabilities   `json:"capabilities,omitempty"`
	SeccompProfile           *SeccompProfile `json:"seccompProfile,omitempty"`
}

// PodSecurityContext holds pod-level security options
type PodSecurityContext struct {
	RunAsNonRoot *bool `json:"runAsNonRoot,omitempty"`
}

// Container describes one container in a Pod
type Container struct {
	Name            string               `json:"name"`
	Image           string               `json:"image"`
	ImagePullPolicy string               `json:"imagePullPolicy,omitempty"`
	Ports           []ContainerPort      `json:"ports,omitempty"`
	Resources       ResourceRequirements `json:"resources,omitempty"`
	StartupProbe    *Probe               `json:"startupProbe,omitempty"`
	LivenessProbe   *Probe               `json:"livenessProbe,omitempty"`
	ReadinessProbe  *Probe               `json:"readinessProbe,omitempty"`
	SecurityContext *SecurityContext     `json:"securityContext,omitempty"`
}

// PodSpec describes the pod's desired behavior
type PodSpec struct {
	AutomountServiceAccountToken *bool               `json:"automountServiceAccountToken,omitempty"`
	Containers                   []Container         `json:"containers"`
	SecurityContext              *PodSecurityContext `json:"securityContext,omitempty"`
}

// PodTemplateSpec is the pod template used in a Deployment
type PodTemplateSpec struct {
	Metadata ObjectMeta `json:"metadata"`
	Spec     PodSpec    `json:"spec"`
}

// LabelSelector is a label query over a set of resources
type LabelSelector struct {
	MatchLabels map[string]string `json:"matchLabels,omitempty"`
}

// DeploymentSpec describes the desired behavior of a Deployment
type DeploymentSpec struct {
	Replicas int             `json:"replicas"`
	Selector LabelSelector   `json:"selector"`
	Template PodTemplateSpec `json:"template"`
}

// Deployment is a Kubernetes apps/v1 Deployment resource
type Deployment struct {
	TypeMeta
	Metadata ObjectMeta     `json:"metadata"`
	Spec     DeploymentSpec `json:"spec"`
}

// ServicePort describes one port in a Service
type ServicePort struct {
	Name       string `json:"name,omitempty"`
	Protocol   string `json:"protocol,omitempty"`
	Port       int    `json:"port"`
	TargetPort int    `json:"targetPort"`
	NodePort   int    `json:"nodePort,omitempty"`
}

// ServiceSpec describes the attributes of a Service
type ServiceSpec struct {
	Type     string            `json:"type,omitempty"`
	Selector map[string]string `json:"selector,omitempty"`
	Ports    []ServicePort     `json:"ports"`
}

// Service is a Kubernetes v1 Service resource
type Service struct {
	TypeMeta
	Metadata ObjectMeta  `json:"metadata"`
	Spec     ServiceSpec `json:"spec"`
}

// IngressPortSpec identifies a port on the backend Service
type IngressPortSpec struct {
	Number int `json:"number"`
}

// IngressServiceBackend references a K8s Service as an Ingress backend
type IngressServiceBackend struct {
	Name string          `json:"name"`
	Port IngressPortSpec `json:"port"`
}

// IngressBackend is the backend for one Ingress path rule
type IngressBackend struct {
	Service IngressServiceBackend `json:"service"`
}

// HTTPIngressPath associates a URL path with a backend
type HTTPIngressPath struct {
	Path     string         `json:"path"`
	PathType string         `json:"pathType"`
	Backend  IngressBackend `json:"backend"`
}

// HTTPIngressRuleValue is the HTTP portion of an IngressRule
type HTTPIngressRuleValue struct {
	Paths []HTTPIngressPath `json:"paths"`
}

// IngressRule maps a host to HTTP path rules
type IngressRule struct {
	Host string                `json:"host,omitempty"`
	HTTP *HTTPIngressRuleValue `json:"http,omitempty"`
}

// IngressTLS describes TLS configuration for an Ingress
type IngressTLS struct {
	Hosts      []string `json:"hosts,omitempty"`
	SecretName string   `json:"secretName,omitempty"`
}

// IngressSpec describes the Ingress the user wishes to exist
type IngressSpec struct {
	IngressClassName *string       `json:"ingressClassName,omitempty"`
	Rules            []IngressRule `json:"rules,omitempty"`
	TLS              []IngressTLS  `json:"tls,omitempty"`
}

// Ingress is a Kubernetes networking.k8s.io/v1 Ingress resource
type Ingress struct {
	TypeMeta
	Metadata ObjectMeta  `json:"metadata"`
	Spec     IngressSpec `json:"spec"`
}
