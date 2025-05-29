package v1alpha3

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"time"
	"unsafe"

	"github.com/cockroachdb/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	applycorev1 "k8s.io/client-go/applyconfigurations/core/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/conversion"
	"sigs.k8s.io/yaml"

	"github.com/redpanda-data/redpanda-operator/charts/redpanda/v5"
	redpandav1alpha2 "github.com/redpanda-data/redpanda-operator/operator/api/redpanda/v1alpha2"
	vectorizedv1alpha1 "github.com/redpanda-data/redpanda-operator/operator/api/vectorized/v1alpha1"
)

// NB: All v1alpha3 and v1alpha2 structs in this file are subject to the
// exhaustruct linter which requires EVERY field of said structs to be
// specified. This will prevent accidental regressions when new fields are
// added by catching issues at lint time.

var _ conversion.Convertible = (*Redpanda)(nil)

func (dst *Redpanda) ConvertFrom(raw conversion.Hub) (err error) { //nolint:staticcheck // changing receiver names makes this much more clear.
	// To make writing conversion code less tedious, convertFrom panics instead
	// of handling errors. We catch them here and convert them to errors.
	defer func() {
		switch r := recover().(type) {
		case nil:
		case error:
			err = errors.Wrap(r, "ConvertFrom failed")
		default:
			err = errors.Newf("ConvertFrom failed with panic: %v", r)
		}
	}()

	src, ok := raw.(*redpandav1alpha2.Redpanda)
	if !ok {
		return errors.Newf("expected dst to be %T; got %T", (*redpandav1alpha2.Redpanda)(nil), raw)
	}

	values, err := src.GetValues()
	if err != nil {
		return err
	}

	*dst = convertFrom[Redpanda](src, &values).(Redpanda)
	return nil
}

func (src *Redpanda) ConvertTo(raw conversion.Hub) (err error) { //nolint:staticcheck // changing receiver names makes this much more clear.
	// To make writing conversion code less tedious, convertTo panics instead
	// of handling errors. We catch them here and convert them to errors.
	defer func() {
		switch r := recover().(type) {
		case nil:
		case error:
			err = errors.Wrap(r, "ConvertTo failed")
		default:
			err = errors.Newf("ConvertTo failed with panic: %v", r)
		}
	}()

	dst, ok := raw.(*redpandav1alpha2.Redpanda)
	if !ok {
		return errors.Newf("expected dst to be %T; got %T", (*redpandav1alpha2.Redpanda)(nil), raw)
	}

	*dst = convertTo[redpandav1alpha2.Redpanda](src).(redpandav1alpha2.Redpanda)
	return nil
}

func convertFrom[T any](src *redpandav1alpha2.Redpanda, values *redpanda.Values) any {
	// NB: We can't perform type switches on generic types _unless_ you do this
	// hack.
	t := reflect.New(reflect.TypeFor[T]()).Elem().Interface()

	// Throughout this function src and values are both referenced using the
	// general mantra: If a value is _truly_ optional, source it from src to
	// preserve it's presence or lack thereof. If a value is required, source
	// it from values to fallback to the default.
	// e.g. Replicas is technically optional as they're nilable on STS's. Where
	// as the RPC listener port is not.
	// The rapid tests, do a surprisingly good good of requiring this behavior
	// to be upheld.

	// Rather than hand writing extremely long method names for each and every
	// A -> B and B <- A conversion and having to plumb around extra context,
	// I've opted to place everything into a giant type switch around the
	// returned typed. I found it easier to implement at first because it
	// bottoms out in a "panic" rather than requiring stubbed out methods with
	// TODO comments. In hindsight, it's quite frustrating to navigate through
	// but it works for now. A future PR may very well refactor this to use the
	// conversion-gen tool.
	switch t.(type) {
	case Redpanda:
		return Redpanda{
			// Specified to sate exhaustruct. This is otherwise the zero value,
			// which is what is generally encountered on a K8s Object not fresh
			// from the API or in a gotohelm chart.
			TypeMeta:   metav1.TypeMeta{APIVersion: "", Kind: ""},
			ObjectMeta: *src.ObjectMeta.DeepCopy(),
			Spec:       convertFrom[RedpandaSpec](src, values).(RedpandaSpec),
			Status:     convertFrom[RedpandaStatus](src, values).(RedpandaStatus),
		}

	case RedpandaSpec:
		return RedpandaSpec{
			ClusterDomain: ptr.Deref(src.Spec.ClusterSpec.ClusterDomain, ""),
			Enterprise:    convertFrom[Enterprise](src, values).(Enterprise),
			ClusterConfig: convertFrom[ClusterConfig](src, values).(ClusterConfig),
			Listeners:     convertFrom[Listeners](src, values).(Listeners),
			NodePoolSpec:  convertFrom[EmbeddedNodePoolSpec](src, values).(EmbeddedNodePoolSpec),
		}

	case Enterprise:
		return Enterprise{
			License: License{
				Value:     values.Enterprise.License,
				ValueFrom: convertFrom[*LicenseValueFrom](src, values).(*LicenseValueFrom),
			},
		}

	case *LicenseValueFrom:
		ref := values.Enterprise.LicenseSecretRef
		// NB: In chart 5.10.x ref defaults to an empty object, so we handle
		// that case as well.
		if ref == nil || ref.Key == "" && ref.Name == "" {
			return (*LicenseValueFrom)(nil)
		}
		return &LicenseValueFrom{
			SecretKeyRef: corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: ref.Name,
				},
				Key: ref.Key,
			},
		}

	case ClusterConfig:
		config := make(ClusterConfig, len(values.Config.Cluster)+len(values.Config.ExtraClusterConfiguration))
		for k, v := range values.Config.Cluster {
			out, err := yaml.Marshal(v)
			if err != nil {
				panic(err)
			}
			config[k] = ValueSource{
				Value:           (*YAMLRepresentation)(ptr.To(string(out))),
				SecretKeyRef:    nil,
				ConfigMapKeyRef: nil,
				UseRawValue:     false,
			}
		}

		for k, v := range values.Config.ExtraClusterConfiguration {
			config[k] = ValueSource{
				Value:           (*YAMLRepresentation)(v.Repr),
				SecretKeyRef:    v.SecretKeyRef.DeepCopy(),
				ConfigMapKeyRef: v.ConfigMapKeyRef.DeepCopy(),
				UseRawValue:     v.UseRawValue,
			}
		}

		return config

	case Listeners:
		return Listeners{
			RPC: RPCListener{
				Port: values.Listeners.RPC.Port,
				TLS:  convertInternalTLSToListenerTLS(&values.TLS, values.Listeners.RPC.TLS),
			},
			Kafka:          convertFromListeners(&values.TLS, values.Listeners.Kafka),
			Admin:          convertFromListeners(&values.TLS, values.Listeners.Admin),
			SchemaRegistry: convertFromListeners(&values.TLS, values.Listeners.SchemaRegistry),
			HTTP:           convertFromListeners(&values.TLS, values.Listeners.HTTP),
		}

	case EmbeddedNodePoolSpec:
		nodeConfig := mapValues(values.Config.Node, func(v any) ValueSource {
			out, err := yaml.Marshal(v)
			if err != nil {
				panic(err)
			}

			return ValueSource{
				Value:           (*YAMLRepresentation)(ptr.To(string(out))),
				ConfigMapKeyRef: nil,
				SecretKeyRef:    nil,
				UseRawValue:     false,
			}
		})

		rpkConfig := mapValues(values.Config.RPK, func(v any) ValueSource {
			out, err := yaml.Marshal(v)
			if err != nil {
				panic(err)
			}

			return ValueSource{
				Value:           (*YAMLRepresentation)(ptr.To(string(out))),
				ConfigMapKeyRef: nil,
				SecretKeyRef:    nil,
				UseRawValue:     false,
			}
		})

		image := ""
		if values.Image.Tag != "" {
			image = fmt.Sprintf("%s:%s", values.Image.Repository, values.Image.Tag)
		} else if repo := get[*string](src.Spec.ClusterSpec, "Image", "Repository"); repo != nil {
			image = *repo
		}

		return EmbeddedNodePoolSpec{
			Replicas: get[*int32](src.Spec.ClusterSpec, "Statefulset", "Replicas"),
			BrokerTemplate: BrokerTemplate{
				Image:                     image,
				RPKConfig:                 rpkConfig,
				NodeConfig:                nodeConfig,
				Resources:                 values.Resources.GetResourceRequirements(),
				ValidateFilesystem:        values.Statefulset.InitContainers.FSValidator.Enabled,
				SetDataDirectoryOwnership: values.Statefulset.InitContainers.SetDataDirOwnership.Enabled,
				PodTemplate:               convertFrom[*PodTemplate](src, values).(*PodTemplate),
			},
		}

	case *PodTemplate:
		pt := get[*redpandav1alpha2.PodTemplate](src.Spec.ClusterSpec, "Statefulset", "PodTemplate")

		if pt == nil {
			return (*PodTemplate)(nil)
		}

		return &PodTemplate{
			ObjectMeta: ObjectMeta{
				Labels:      pt.Labels,
				Annotations: pt.Annotations,
			},
			Spec: (*applycorev1.PodSpecApplyConfiguration)(pt.Spec),
		}

	case RedpandaStatus:
		return RedpandaStatus{
			Conditions:    src.Status.Conditions,
			ConfigVersion: src.Status.ConfigVersion,
			LicenseStatus: (*LicenseStatus)(src.Status.LicenseStatus),
			NodePools: doMap(src.Status.NodePools, func(src redpandav1alpha2.NodePoolStatus) NodePoolStatus {
				return NodePoolStatus{
					Name:              src.Name,
					Replicas:          src.Replicas,
					DesiredReplicas:   src.DesiredReplicas,
					OutOfDateReplicas: src.OutOfDateReplicas,
					UpToDateReplicas:  src.UpToDateReplicas,
					CondemnedReplicas: src.CondemnedReplicas,
					ReadyReplicas:     src.ReadyReplicas,
					RunningReplicas:   src.RunningReplicas,
				}
			}),
		}

	default:
		panic(fmt.Sprintf("unhandled type in convFrom: %T", t))
	}
}

func convertTo[T any](src *Redpanda) any {
	// NB: We can't perform type switches on generic types _unless_ you do this
	// hack.
	t := reflect.New(reflect.TypeFor[T]()).Elem().Interface()

	// Rather than hand writing extremely long method names for each and every
	// A -> B and B <- A conversion and having to plumb around extra context,
	// I've opted to place everything into a giant type switch around the
	// returned typed. I found it easier to implement at first because it
	// bottoms out in a "panic" rather than requiring stubbed out methods with
	// TODO comments. In hindsight, it's quite frustrating to navigate through
	// but it works for now. A future PR may very well refactor this to use the
	// conversion-gen tool.
	switch t.(type) {
	case redpandav1alpha2.Redpanda:
		return redpandav1alpha2.Redpanda{
			// Specified to sate exhaustruct. This is otherwise the zero value,
			// which is what is generally encountered on a K8s Object not fresh
			// from the API or in a gotohelm chart.
			TypeMeta:   metav1.TypeMeta{APIVersion: "", Kind: ""},
			ObjectMeta: *src.ObjectMeta.DeepCopy(),
			Spec: redpandav1alpha2.RedpandaSpec{
				ChartRef: redpandav1alpha2.ChartRef{
					ChartName:          "",
					ChartVersion:       "",
					HelmRepositoryName: "",
					Timeout:            nil,
					Upgrade:            nil,
					UseFlux:            ptr.To(false),
				}, // TODO extract me from an annotation.
				ClusterSpec: convertTo[*redpandav1alpha2.RedpandaClusterSpec](src).(*redpandav1alpha2.RedpandaClusterSpec),
				Migration:   nil, // Unsupported for a long time now.
			},
			Status: convertTo[redpandav1alpha2.RedpandaStatus](src).(redpandav1alpha2.RedpandaStatus),
		}

	case *redpandav1alpha2.RedpandaClusterSpec:
		nodeConfig := toRawExtension(mapFilterValues(src.Spec.NodePoolSpec.BrokerTemplate.NodeConfig, func(v ValueSource) (any, bool) {
			if v.Value == nil {
				return nil, false
			}

			// TODO Handle all cases here
			var value any
			if err := yaml.Unmarshal([]byte(*v.Value), &value); err != nil {
				panic(err)
			}
			return value, true
		}))

		rpkConfig := toRawExtension(mapFilterValues(src.Spec.NodePoolSpec.BrokerTemplate.RPKConfig, func(v ValueSource) (any, bool) {
			if v.Value == nil {
				return nil, false
			}

			// TODO Handle all cases here
			var value any
			if err := yaml.Unmarshal([]byte(*v.Value), &value); err != nil {
				panic(err)
			}
			return value, true
		}))

		var image *redpandav1alpha2.RedpandaImage
		if idx := strings.LastIndex(src.Spec.NodePoolSpec.BrokerTemplate.Image, ""); idx != -1 {
			image = &redpandav1alpha2.RedpandaImage{
				Repository: ptr.To(src.Spec.NodePoolSpec.BrokerTemplate.Image[:idx]),
				Tag:        ptr.To(src.Spec.NodePoolSpec.BrokerTemplate.Image[idx:]),
				PullPolicy: nil,
			}
		} else if src.Spec.NodePoolSpec.BrokerTemplate.Image != "" {
			image = &redpandav1alpha2.RedpandaImage{
				Repository: ptr.To(src.Spec.NodePoolSpec.BrokerTemplate.Image),
				Tag:        nil,
				PullPolicy: nil,
			}
		}

		return &redpandav1alpha2.RedpandaClusterSpec{
			Image:         image,
			Enterprise:    convertTo[*redpandav1alpha2.Enterprise](src).(*redpandav1alpha2.Enterprise),
			ClusterDomain: nilIfZero(src.Spec.ClusterDomain),
			Config: &redpandav1alpha2.Config{
				RPK:  rpkConfig,
				Node: nodeConfig,

				// All ClusterConfig gets pushed into ExtraClusterConfiguration
				Tunable:                   nil,
				Cluster:                   nil,
				ExtraClusterConfiguration: convertTo[redpandav1alpha2.ClusterConfiguration](src).(redpandav1alpha2.ClusterConfiguration),

				// Unused fields
				SchemaRegistryClient: nil,
				PandaProxyClient:     nil,
			},
			TLS:         convertTo[*redpandav1alpha2.TLS](src).(*redpandav1alpha2.TLS),
			Listeners:   convertTo[*redpandav1alpha2.Listeners](src).(*redpandav1alpha2.Listeners),
			Statefulset: convertTo[*redpandav1alpha2.Statefulset](src).(*redpandav1alpha2.Statefulset),
			Resources: &redpandav1alpha2.Resources{
				Limits:   (*map[corev1.ResourceName]resource.Quantity)(&src.Spec.NodePoolSpec.BrokerTemplate.Resources.Limits),
				Requests: (*map[corev1.ResourceName]resource.Quantity)(&src.Spec.NodePoolSpec.BrokerTemplate.Resources.Requests),
				CPU:      nil,
				Memory:   nil,
			},

			// TODO Audit this list.
			Affinity:                   nil,
			AuditLogging:               nil,
			Auth:                       nil,
			CommonLabels:               nil,
			Connectors:                 nil,
			Console:                    nil,
			DeprecatedFullNameOverride: "",
			External:                   nil,
			Force:                      nil,
			FullnameOverride:           nil,
			ImagePullSecrets:           nil,
			LicenseKey:                 nil,
			LicenseSecretRef:           nil,
			Logging:                    nil,
			Monitoring:                 nil,
			NameOverride:               nil,
			NodeSelector:               nil,
			PostInstallJob:             nil,
			PostUpgradeJob:             nil,
			RBAC:                       nil,
			RackAwareness:              nil,
			Service:                    nil,
			ServiceAccount:             nil,
			Storage:                    nil,
			Tests:                      nil,
			Tolerations:                nil,
			Tuning:                     nil,
		}

	case redpandav1alpha2.ClusterConfiguration:
		return redpandav1alpha2.ClusterConfiguration(mapValues(src.Spec.ClusterConfig, convertFromValueSource))

	case *redpandav1alpha2.Enterprise:
		var ref *redpandav1alpha2.EnterpriseLicenseSecretRef
		if value := src.Spec.Enterprise.License.ValueFrom; value != nil {
			ref = &redpandav1alpha2.EnterpriseLicenseSecretRef{
				Key:  ptr.To(value.SecretKeyRef.Key),
				Name: ptr.To(value.SecretKeyRef.Name),
			}
		}
		return &redpandav1alpha2.Enterprise{
			License:          ptr.To(src.Spec.Enterprise.License.Value),
			LicenseSecretRef: ref,
		}

	case *redpandav1alpha2.TLS:
		certs := map[string]*redpandav1alpha2.Certificate{}

		if src.Spec.Listeners.RPC.TLS != nil {
			certs["rpc"] = convertListenerTLSToCertificate(src.Spec.Listeners.RPC.TLS)
		}

		for _, l := range src.Spec.Listeners.Admin {
			if l.TLS == nil {
				continue
			}

			certs["admin-"+l.Name] = convertListenerTLSToCertificate(l.TLS)
		}

		for _, l := range src.Spec.Listeners.Kafka {
			if l.TLS == nil {
				continue
			}

			certs["kafka-"+l.Name] = convertListenerTLSToCertificate(l.TLS)
		}

		for _, l := range src.Spec.Listeners.HTTP {
			if l.TLS == nil {
				continue
			}

			certs["http-"+l.Name] = convertListenerTLSToCertificate(l.TLS)
		}

		for _, l := range src.Spec.Listeners.SchemaRegistry {
			if l.TLS == nil {
				continue
			}

			certs["schemaregistry-"+l.Name] = convertListenerTLSToCertificate(l.TLS)
		}

		return &redpandav1alpha2.TLS{
			Enabled: nil,
			Certs:   certs,
		}

	case *redpandav1alpha2.Listeners:
		// TODO remove this once we move to defaulting?
		// Mirrors the default in values.yaml of v2.4.x.
		kakfaInternal, kafkaExternal := convertToListeners("kafka", src.Spec.Listeners.Kafka, Listener{
			Name:                 "internal",
			Port:                 9093,
			AuthenticationMethod: nil,
			AdvertisedPorts:      nil,
			PrefixTemplate:       nil,
			TLS: &ListenerTLS{
				TrustStore:        nil,
				RequireClientAuth: false,
				CertificateSource: CertificateSource{
					CertManager: &CertManagerCertificateSource{Duration: nil},
					IssuerRef:   nil,
					Secrets:     nil,
				},
			},
		})

		adminInternal, adminExternal := convertToListeners("admin", src.Spec.Listeners.Admin, Listener{
			Name:                 "internal",
			Port:                 9644,
			AuthenticationMethod: nil,
			AdvertisedPorts:      nil,
			PrefixTemplate:       nil,
			TLS: &ListenerTLS{
				TrustStore:        nil,
				RequireClientAuth: false,
				CertificateSource: CertificateSource{
					CertManager: &CertManagerCertificateSource{Duration: nil},
					IssuerRef:   nil,
					Secrets:     nil,
				},
			},
		})

		srInternal, srExternal := convertToListeners("schemaregistry", src.Spec.Listeners.SchemaRegistry, Listener{
			Name:                 "internal",
			Port:                 8081,
			AuthenticationMethod: nil,
			AdvertisedPorts:      nil,
			PrefixTemplate:       nil,
			TLS: &ListenerTLS{
				TrustStore:        nil,
				RequireClientAuth: false,
				CertificateSource: CertificateSource{
					CertManager: &CertManagerCertificateSource{Duration: nil},
					IssuerRef:   nil,
					Secrets:     nil,
				},
			},
		})

		httpInternal, httpExternal := convertToListeners("http", src.Spec.Listeners.HTTP, Listener{
			Name:                 "internal",
			Port:                 8082,
			AuthenticationMethod: nil,
			AdvertisedPorts:      nil,
			PrefixTemplate:       nil,
			TLS: &ListenerTLS{
				TrustStore:        nil,
				RequireClientAuth: false,
				CertificateSource: CertificateSource{
					CertManager: &CertManagerCertificateSource{Duration: nil},
					IssuerRef:   nil,
					Secrets:     nil,
				},
			},
		})

		return &redpandav1alpha2.Listeners{
			RPC: &redpandav1alpha2.RPC{
				Port: ptr.To(src.Spec.Listeners.RPC.Port),
				TLS:  convertListenerTLSToListenerTLS("rpc", src.Spec.Listeners.RPC.TLS),
			},
			Kafka: &redpandav1alpha2.Kafka{
				Listener: kakfaInternal,
				External: kafkaExternal,
			},
			Admin: &redpandav1alpha2.Admin{
				Listener:    adminInternal,
				External:    adminExternal,
				AppProtocol: nil,
			},
			SchemaRegistry: &redpandav1alpha2.SchemaRegistry{
				Listener:      srInternal,
				External:      srExternal,
				KafkaEndpoint: nil,
			},
			HTTP: &redpandav1alpha2.HTTP{
				Listener:      httpInternal,
				External:      httpExternal,
				KafkaEndpoint: nil,
			},
		}

	case *redpandav1alpha2.Statefulset:
		return &redpandav1alpha2.Statefulset{
			InitContainers: &redpandav1alpha2.InitContainers{
				SetDataDirOwnership: &redpandav1alpha2.SetDataDirOwnership{
					Enabled:           ptr.To(src.Spec.NodePoolSpec.BrokerTemplate.SetDataDirectoryOwnership),
					Resources:         nil,
					ExtraVolumeMounts: nil,
				},
				FsValidator: &redpandav1alpha2.FsValidator{
					Enabled:           ptr.To(src.Spec.NodePoolSpec.BrokerTemplate.ValidateFilesystem),
					Resources:         nil,
					ExtraVolumeMounts: nil,
					ExpectedFS:        nil,
				},
				Configurator:                      nil,
				ExtraInitContainers:               nil,
				SetTieredStorageCacheDirOwnership: nil,
				Tuning:                            nil,
			},
			Replicas:    src.Spec.NodePoolSpec.Replicas,
			PodTemplate: convertTo[*redpandav1alpha2.PodTemplate](src).(*redpandav1alpha2.PodTemplate),

			AdditionalRedpandaCmdFlags:    nil,
			AdditionalSelectorsLabels:     nil,
			Annotations:                   nil,
			Budget:                        nil,
			ExtraVolumeMounts:             nil,
			ExtraVolumes:                  nil,
			InitContainerImage:            nil,
			LivenessProbe:                 nil,
			NodeSelector:                  nil,
			PodAffinity:                   nil,
			PodAntiAffinity:               nil,
			PriorityClassName:             nil,
			ReadinessProbe:                nil,
			SecurityContext:               nil,
			SideCars:                      nil,
			SkipChown:                     nil,
			StartupProbe:                  nil,
			TerminationGracePeriodSeconds: nil,
			Tolerations:                   nil,
			TopologySpreadConstraints:     nil,
			UpdateStrategy:                nil,
		}

	case *redpandav1alpha2.PodTemplate:
		if src.Spec.NodePoolSpec.BrokerTemplate.PodTemplate == nil {
			return (*redpandav1alpha2.PodTemplate)(nil)
		}

		return &redpandav1alpha2.PodTemplate{
			Labels:      src.Spec.NodePoolSpec.BrokerTemplate.PodTemplate.Labels,
			Annotations: src.Spec.NodePoolSpec.BrokerTemplate.PodTemplate.Annotations,
			Spec:        (*redpandav1alpha2.PodSpecApplyConfiguration)(src.Spec.NodePoolSpec.BrokerTemplate.PodTemplate.Spec),
		}

	case redpandav1alpha2.RedpandaStatus:
		return redpandav1alpha2.RedpandaStatus{
			Conditions:    src.Status.Conditions,
			ConfigVersion: src.Status.ConfigVersion,
			LicenseStatus: (*redpandav1alpha2.RedpandaLicenseStatus)(src.Status.LicenseStatus),
			NodePools: doMap(src.Status.NodePools, func(src NodePoolStatus) redpandav1alpha2.NodePoolStatus {
				return redpandav1alpha2.NodePoolStatus{
					Name:              src.Name,
					Replicas:          src.Replicas,
					DesiredReplicas:   src.DesiredReplicas,
					OutOfDateReplicas: src.OutOfDateReplicas,
					UpToDateReplicas:  src.UpToDateReplicas,
					CondemnedReplicas: src.CondemnedReplicas,
					ReadyReplicas:     src.ReadyReplicas,
					RunningReplicas:   src.RunningReplicas,
				}
			}),
			// Deprecated fields, specified to sate exhaustruct.
			ObservedGeneration:         0,
			LastHandledReconcileAt:     "",
			LastAppliedRevision:        "",
			LastAttemptedRevision:      "",
			HelmRelease:                "",
			HelmReleaseReady:           nil,
			HelmRepository:             "",
			HelmRepositoryReady:        nil,
			UpgradeFailures:            0,
			Failures:                   0,
			InstallFailures:            0,
			ManagedDecommissioningNode: nil,
		}

	default:
		panic(fmt.Sprintf("unhandled type in convFrom: %T", t))
	}
}

func convertToListeners(kind string, src []Listener, defaultInternal Listener) (redpandav1alpha2.Listener, map[string]*redpandav1alpha2.ExternalListener) {
	internal := defaultInternal
	idx := slices.IndexFunc(src, func(l Listener) bool {
		return l.Name == "internal"
	})

	if idx != -1 {
		internal = src[idx]
	}

	external := map[string]*redpandav1alpha2.ExternalListener{}
	for i, l := range src {
		if i == idx {
			continue
		}

		external[l.Name] = &redpandav1alpha2.ExternalListener{
			Listener: redpandav1alpha2.Listener{
				Enabled:              ptr.To(true),
				Port:                 ptr.To(l.Port),
				AuthenticationMethod: (*string)(l.AuthenticationMethod),
				PrefixTemplate:       l.PrefixTemplate,
				TLS:                  convertListenerTLSToListenerTLS(fmt.Sprintf("%s-%s", kind, l.Name), l.TLS),
			},
			NodePort:        nil, // TODO
			AdvertisedPorts: l.AdvertisedPorts,
		}
	}

	// If a listener named "default" hasn't been specified, disable the default
	// listener.
	if _, ok := external["default"]; !ok {
		external["default"] = &redpandav1alpha2.ExternalListener{
			Listener: redpandav1alpha2.Listener{
				Enabled:              ptr.To(false),
				Port:                 nil,
				TLS:                  nil,
				PrefixTemplate:       nil,
				AuthenticationMethod: nil,
			},
			AdvertisedPorts: nil,
			NodePort:        nil,
		}
	}

	return redpandav1alpha2.Listener{
		Enabled:              ptr.To(true),
		Port:                 ptr.To(internal.Port),
		AuthenticationMethod: (*string)(internal.AuthenticationMethod),
		PrefixTemplate:       internal.PrefixTemplate,
		TLS:                  convertListenerTLSToListenerTLS(fmt.Sprintf("%s-%s", kind, internal.Name), internal.TLS),
	}, external
}

//nolint:gosec // unsafe is just for fancy type casting
func convertFromListeners[T ~string](tls *redpanda.TLS, cfg redpanda.ListenerConfig[T]) []Listener {
	listeners := make([]Listener, 1+len(cfg.External))

	listeners[0] = Listener{
		Name:                 "internal",
		Port:                 cfg.Port,
		AuthenticationMethod: (*AuthenticationMethod)(unsafe.Pointer(cfg.AuthenticationMethod)),
		TLS:                  convertInternalTLSToListenerTLS(tls, cfg.TLS),

		// Unsupported on the internal listener in v1alpha2.
		AdvertisedPorts: nil,
		PrefixTemplate:  nil,
	}

	i := 0
	for _, name := range slices.Sorted(maps.Keys(cfg.External)) {
		l := cfg.External[name]

		if !l.IsEnabled() {
			continue
		}

		i++

		var listenerTLS *ListenerTLS
		if l.TLS != nil && l.TLS.IsEnabled(&cfg.TLS, tls) {
			// InternalTLS and ExternalTLS are effectively the same type sans
			// optionality of fields. To dry this up, we just upcast external
			// to internal.
			listenerTLS = convertInternalTLSToListenerTLS(tls, redpanda.InternalTLS{
				Enabled:           ptr.To(true),
				Cert:              ptr.Deref(l.TLS.Cert, ""),
				RequireClientAuth: ptr.Deref(l.TLS.RequireClientAuth, false),
				TrustStore:        l.TLS.TrustStore,
			})
		}

		listeners[i] = Listener{
			Name:                 name,
			Port:                 l.Port,
			AdvertisedPorts:      l.AdvertisedPorts,
			PrefixTemplate:       l.PrefixTemplate,
			AuthenticationMethod: (*AuthenticationMethod)(unsafe.Pointer(l.AuthenticationMethod)),
			TLS:                  listenerTLS,
		}
	}

	return listeners[:i+1]
}

func convertListenerTLSToCertificate(tls *ListenerTLS) *redpandav1alpha2.Certificate {
	if tls.IssuerRef != nil {
		return &redpandav1alpha2.Certificate{
			Enabled: ptr.To(true),
			IssuerRef: &redpandav1alpha2.IssuerRef{
				Name:  ptr.To(tls.IssuerRef.Name),
				Kind:  (*string)(ptr.To(tls.IssuerRef.Kind)),
				Group: nil,
			},
			ApplyInternalDNSNames: nil,
			CAEnabled:             nil,
			ClientSecretRef:       nil,
			Duration:              nil,
			SecretRef:             nil,
		}
	}

	if tls.Secrets != nil {
		var clientRef *redpandav1alpha2.SecretRef
		if tls.Secrets.ClientSecretRef != nil {
			clientRef = &redpandav1alpha2.SecretRef{
				Name: ptr.To(tls.Secrets.ClientSecretRef.Name),
			}
		}

		return &redpandav1alpha2.Certificate{
			Enabled: ptr.To(true),
			SecretRef: &redpandav1alpha2.SecretRef{
				Name: ptr.To(tls.Secrets.SecretRef.Name),
			},
			ApplyInternalDNSNames: nil,
			CAEnabled:             nil,
			ClientSecretRef:       clientRef,
			Duration:              nil,
			IssuerRef:             nil,
		}
	}

	return &redpandav1alpha2.Certificate{
		Enabled:               ptr.To(true),
		Duration:              tls.CertManager.Duration,
		ApplyInternalDNSNames: nil,
		CAEnabled:             nil,
		ClientSecretRef:       nil,
		IssuerRef:             nil,
		SecretRef:             nil,
	}
}

func convertListenerTLSToListenerTLS(name string, tls *ListenerTLS) *redpandav1alpha2.ListenerTLS {
	if tls == nil {
		// NB: We have to "explicitly" disable TLS rather than returning nil as
		// the default helm values might merge in an re-enable TLS. e.g. in the
		// case of the "default" certs.
		return &redpandav1alpha2.ListenerTLS{
			Enabled:           ptr.To(false),
			Cert:              nil,
			RequireClientAuth: nil,
			SecretRef:         nil,
			TrustStore:        nil,
		}
	}

	var truststore *redpandav1alpha2.TrustStore
	if tls.TrustStore != nil {
		truststore = &redpandav1alpha2.TrustStore{
			SecretKeyRef:    tls.TrustStore.SecretKeyRef,
			ConfigMapKeyRef: tls.TrustStore.ConfigMapKeyRef,
		}
	}

	return &redpandav1alpha2.ListenerTLS{
		Enabled:           ptr.To(true),
		Cert:              ptr.To(name),
		RequireClientAuth: ptr.To(tls.RequireClientAuth),
		TrustStore:        truststore,
		SecretRef:         nil,
	}
}

func convertInternalTLSToListenerTLS(tls *redpanda.TLS, config redpanda.InternalTLS) *ListenerTLS {
	if !config.IsEnabled(tls) {
		return nil
	}

	cert, ok := tls.Certs[config.Cert]
	if !ok {
		panic(errors.Newf("cert %q missing from tls.certs", config.Cert))
	}

	var source CertificateSource
	if cert.IssuerRef != nil {
		source = CertificateSource{
			Secrets:     nil,
			CertManager: nil,
			IssuerRef: &IssuerRefCertificateSource{
				Name: cert.IssuerRef.Name,
				Kind: IssuerKind(cert.IssuerRef.Kind),
			},
		}
	} else if cert.SecretRef != nil {
		source = CertificateSource{
			IssuerRef:   nil,
			CertManager: nil,
			Secrets: &SecretCertificateSource{
				ClientSecretRef: cert.ClientSecretRef,
				SecretRef:       *cert.SecretRef,
			},
		}
	} else {
		var duration *metav1.Duration
		if cert.Duration != "" {
			d, err := time.ParseDuration(cert.Duration)
			if err != nil {
				panic(errors.Newf("invalid duration in certificate %q: %q", config.Cert, cert.Duration))
			}
			duration = &metav1.Duration{Duration: d}
		}

		source = CertificateSource{
			Secrets:   nil,
			IssuerRef: nil,
			CertManager: &CertManagerCertificateSource{
				Duration: duration,
			},
		}
	}

	var truststore *TrustStore
	if config.TrustStore != nil {
		truststore = &TrustStore{
			SecretKeyRef:    config.TrustStore.SecretKeyRef,
			ConfigMapKeyRef: config.TrustStore.ConfigMapKeyRef,
		}
	}

	return &ListenerTLS{
		TrustStore:        truststore,
		RequireClientAuth: config.RequireClientAuth,
		CertificateSource: source,
	}
}

func convertFromValueSource(v ValueSource) vectorizedv1alpha1.ClusterConfigValue {
	return vectorizedv1alpha1.ClusterConfigValue{
		Repr:            (*vectorizedv1alpha1.YAMLRepresentation)(v.Value),
		ConfigMapKeyRef: v.ConfigMapKeyRef,
		SecretKeyRef:    v.SecretKeyRef,
		UseRawValue:     v.UseRawValue,
	}
}

func nilIfZero[T comparable](in T) *T {
	var zero T
	if in == zero {
		return nil
	}
	return &in
}

func toRawExtension(in any) *runtime.RawExtension {
	if in == nil {
		return nil
	}

	out, err := json.Marshal(in)
	if err != nil {
		panic(err)
	}
	return &runtime.RawExtension{
		Raw: out,
	}
}

func doMap[T any, O any](s []T, fn func(T) O) []O {
	if s == nil {
		return nil
	}
	out := make([]O, len(s))
	for i := range s {
		out[i] = fn(s[i])
	}
	return out
}

func get[T any](root any, keys ...string) T {
	value := reflect.ValueOf(root)
	for _, key := range keys {
		switch value.Kind() {
		case reflect.Pointer:
			if value.IsNil() {
				var zero T
				return zero
			}

			value = value.Elem()
		}

		value = value.FieldByName(key)
	}

	return value.Interface().(T)
}

func mapValues[K comparable, I any, O any](in map[K]I, fn func(I) O) map[K]O {
	if in == nil {
		return nil
	}

	out := make(map[K]O, len(in))
	for k, v := range in {
		out[k] = fn(v)
	}
	return out
}

func mapFilterValues[K comparable, I any, O any](in map[K]I, fn func(I) (O, bool)) map[K]O {
	out := make(map[K]O, len(in))
	for k, v := range in {
		if converted, ok := fn(v); ok {
			out[k] = converted
		}
	}
	return out
}
