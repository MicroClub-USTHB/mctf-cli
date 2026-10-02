package kubernetes

import (
	"fmt"
	"strings"

	"sigs.k8s.io/yaml"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
)

// ManifestsYAML renders a Deployment + Service + optional Ingress as a
// multi-document YAML string (documents separated by "---\n"). TCP
// challenges get a NodePort Service instead of ClusterIP + Ingress.
func ManifestsYAML(u KubeUnit) ([]byte, error) {
	dep := buildDeployment(u)
	svc := buildService(u)

	depBytes, err := yaml.Marshal(dep)
	if err != nil {
		return nil, fmt.Errorf("marshal Deployment: %w", err)
	}
	svcBytes, err := yaml.Marshal(svc)
	if err != nil {
		return nil, fmt.Errorf("marshal Service: %w", err)
	}

	out := append([]byte("---\n"), depBytes...)
	out = append(out, []byte("---\n")...)
	out = append(out, svcBytes...)

	if !u.TCP && u.Subdomain != "" && u.Domain != "" {
		ing := buildIngress(u)
		ingBytes, err := yaml.Marshal(ing)
		if err != nil {
			return nil, fmt.Errorf("marshal Ingress: %w", err)
		}
		out = append(out, []byte("---\n")...)
		out = append(out, ingBytes...)
	}

	return out, nil
}

func buildDeployment(u KubeUnit) Deployment {
	labels := map[string]string{"app": u.Name}

	container := Container{
		Name:  u.Name,
		Image: u.Image,
		Ports: []ContainerPort{{ContainerPort: u.InternalPort, Protocol: "TCP"}},
	}

	if r := u.Resources; r != nil {
		req := ResourceList{}
		lim := ResourceList{}
		if r.CPU != "" {
			req["cpu"] = r.CPU
			lim["cpu"] = doubleCPU(r.CPU)
		}
		if r.Memory != "" {
			req["memory"] = r.Memory
			lim["memory"] = r.Memory
		}
		if len(req) > 0 || len(lim) > 0 {
			container.Resources = ResourceRequirements{Limits: lim, Requests: req}
		}
	}

	if u.Healthcheck != nil {
		p := buildProbe(u.Healthcheck, u.InternalPort)
		container.LivenessProbe = p
		container.ReadinessProbe = p
	}

	return Deployment{
		TypeMeta: TypeMeta{APIVersion: "apps/v1", Kind: "Deployment"},
		Metadata: ObjectMeta{Name: u.Name, Namespace: u.Namespace, Labels: labels},
		Spec: DeploymentSpec{
			Replicas: 1,
			Selector: LabelSelector{MatchLabels: labels},
			Template: PodTemplateSpec{
				Metadata: ObjectMeta{Labels: labels},
				Spec:     PodSpec{Containers: []Container{container}},
			},
		},
	}
}

func doubleCPU(cpu string) string {
	if strings.HasSuffix(cpu, "m") {
		var n int
		if _, err := fmt.Sscanf(cpu, "%dm", &n); err == nil && n > 0 {
			return fmt.Sprintf("%dm", n*2)
		}
	}
	var f float64
	if _, err := fmt.Sscanf(cpu, "%f", &f); err == nil && f > 0 {
		val := f * 2
		if val == float64(int(val)) {
			return fmt.Sprintf("%d", int(val))
		}
		return fmt.Sprintf("%.2f", val)
	}
	return cpu
}

func buildService(u KubeUnit) Service {
	labels := map[string]string{"app": u.Name}
	svcType := "ClusterIP"
	if u.TCP {
		svcType = "NodePort"
	}
	sp := ServicePort{
		Name:       "main",
		Protocol:   "TCP",
		Port:       u.InternalPort,
		TargetPort: u.InternalPort,
	}
	if u.TCP && u.NodePort > 0 {
		sp.NodePort = u.NodePort
	}
	return Service{
		TypeMeta: TypeMeta{APIVersion: "v1", Kind: "Service"},
		Metadata: ObjectMeta{Name: u.Name, Namespace: u.Namespace, Labels: labels},
		Spec: ServiceSpec{
			Type:     svcType,
			Selector: labels,
			Ports:    []ServicePort{sp},
		},
	}
}

func buildIngress(u KubeUnit) Ingress {
	host := u.Subdomain + "." + u.Domain
	labels := map[string]string{"app": u.Name}

	var ingressClass *string
	if u.IngressClass != "" {
		ic := u.IngressClass
		ingressClass = &ic
	}

	ing := Ingress{
		TypeMeta: TypeMeta{APIVersion: "networking.k8s.io/v1", Kind: "Ingress"},
		Metadata: ObjectMeta{Name: u.Name, Namespace: u.Namespace, Labels: labels},
		Spec: IngressSpec{
			IngressClassName: ingressClass,
			Rules: []IngressRule{{
				Host: host,
				HTTP: &HTTPIngressRuleValue{
					Paths: []HTTPIngressPath{{
						Path:     "/",
						PathType: "Prefix",
						Backend: IngressBackend{
							Service: IngressServiceBackend{
								Name: u.Name,
								Port: IngressPortSpec{Number: u.InternalPort},
							},
						},
					}},
				},
			}},
		},
	}

	if !u.NoSSL {
		ing.Spec.TLS = []IngressTLS{{
			Hosts:      []string{host},
			SecretName: u.Name + "-tls",
		}}
	}

	return ing
}

func buildProbe(h *challenge.Healthcheck, port int) *Probe {
	if h == nil {
		return nil
	}
	interval := parseSeconds(h.Interval, 10)
	p := &Probe{
		InitialDelaySeconds: interval,
		PeriodSeconds:       interval,
		TimeoutSeconds:      parseSeconds(h.Timeout, 5),
		FailureThreshold:    h.Retries,
	}
	switch h.Type {
	case "http":
		p.HTTPGet = &HTTPGetAction{Path: h.Path, Port: port}
	case "cmd":
		p.Exec = &ExecAction{Command: h.Cmd}
	}
	return p
}
