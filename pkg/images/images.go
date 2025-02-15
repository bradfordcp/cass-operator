// Copyright DataStax, Inc.
// Please see the included license file for details.

package images

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"

	configv1beta1 "github.com/k8ssandra/cass-operator/apis/config/v1beta1"
	"github.com/pkg/errors"
)

var (
	imageConfig *configv1beta1.ImageConfig
	scheme      = runtime.NewScheme()
)

const (
	ValidDseVersionRegexp      = "(6\\.8\\.\\d+)|(7\\.\\d+\\.\\d+)"
	ValidOssVersionRegexp      = "(3\\.11\\.\\d+)|(4\\.\\d+\\.\\d+)|(5\\.\\d+\\.\\d+)"
	DefaultCassandraRepository = "k8ssandra/cass-management-api"
	DefaultDSERepository       = "datastax/dse-server"
)

func init() {
	utilruntime.Must(configv1beta1.AddToScheme(scheme))
}

func ParseImageConfig(imageConfigFile string) error {
	content, err := os.ReadFile(imageConfigFile)
	if err != nil {
		return fmt.Errorf("could not read file at %s", imageConfigFile)
	}

	if _, err = LoadImageConfig(content); err != nil {
		err = errors.Wrapf(err, "unable to load imageConfig from %s", imageConfigFile)
		return err
	}

	return nil
}

func LoadImageConfig(content []byte) (*configv1beta1.ImageConfig, error) {
	codecs := serializer.NewCodecFactory(scheme)

	parsedImageConfig := &configv1beta1.ImageConfig{}
	if err := runtime.DecodeInto(codecs.UniversalDecoder(), content, parsedImageConfig); err != nil {
		return nil, fmt.Errorf("could not decode file into runtime.Object: %v", err)
	}

	imageConfig = parsedImageConfig

	return parsedImageConfig, nil
}

func IsDseVersionSupported(version string) bool {
	validVersions := regexp.MustCompile(ValidDseVersionRegexp)
	return validVersions.MatchString(version)
}

func IsOssVersionSupported(version string) bool {
	validVersions := regexp.MustCompile(ValidOssVersionRegexp)
	return validVersions.MatchString(version)
}

func stripRegistry(image string) string {
	comps := strings.Split(image, "/")

	if len(comps) > 1 && (strings.Contains(comps[0], ".") || strings.Contains(comps[0], ":")) {
		return strings.Join(comps[1:], "/")
	} else {
		return image
	}
}

func applyDefaultRegistryOverride(image string) string {
	customRegistry := GetImageConfig().ImageRegistry
	customRegistry = strings.TrimSuffix(customRegistry, "/")

	if customRegistry == "" {
		return image
	} else {
		imageNoRegistry := stripRegistry(image)
		return fmt.Sprintf("%s/%s", customRegistry, imageNoRegistry)
	}
}

func ApplyRegistry(image string) string {
	return applyDefaultRegistryOverride(image)
}

func GetImageConfig() *configv1beta1.ImageConfig {
	// For now, this is static configuration (updated only on start of the pod), even if the actual ConfigMap underneath is updated.
	return imageConfig
}

func getCassandraContainerImageOverride(serverType, version string) (bool, string) {
	// If there's a mapped volume with overrides in the ConfigMap, use these. Otherwise only calculate
	images := GetImageConfig().Images
	if images != nil {
		if serverType == "dse" {
			if value, found := images.DSEVersions[version]; found {
				return true, value
			}
		}
		if serverType == "cassandra" {
			if value, found := images.CassandraVersions[version]; found {
				return true, value
			}
		}
	}
	return false, ""
}

func getImageComponents(serverType string) (string, string) {
	var defaultPrefix string

	if serverType == "dse" {
		defaultPrefix = DefaultDSERepository
	}
	if serverType == "cassandra" {
		defaultPrefix = DefaultCassandraRepository
	}

	defaults := GetImageConfig().DefaultImages
	if defaults != nil {
		var component configv1beta1.ImageComponent
		if serverType == "dse" {
			component = defaults.DSEImageComponent
		}
		if serverType == "cassandra" {
			component = defaults.CassandraImageComponent
		}

		if component.Repository != "" {
			return component.Repository, component.Suffix
		}
	}

	return defaultPrefix, ""
}

func GetCassandraImage(serverType, version string) (string, error) {
	if found, image := getCassandraContainerImageOverride(serverType, version); found {
		return ApplyRegistry(image), nil
	}

	if serverType == "dse" {
		if !IsDseVersionSupported(version) {
			return "", fmt.Errorf("server 'dse' and version '%s' do not work together", version)
		}
	} else {
		if !IsOssVersionSupported(version) {
			return "", fmt.Errorf("server 'cassandra' and version '%s' do not work together", version)
		}
	}

	prefix, suffix := getImageComponents(serverType)

	return ApplyRegistry(fmt.Sprintf("%s:%s%s", prefix, version, suffix)), nil
}

func GetConfigBuilderImage() string {
	return ApplyRegistry(GetImageConfig().Images.ConfigBuilder)
}

func GetSystemLoggerImage() string {
	return ApplyRegistry(GetImageConfig().Images.SystemLogger)
}

func AddDefaultRegistryImagePullSecrets(podSpec *corev1.PodSpec) bool {
	secretName := GetImageConfig().ImagePullSecret.Name
	if secretName != "" {
		podSpec.ImagePullSecrets = append(
			podSpec.ImagePullSecrets,
			corev1.LocalObjectReference{Name: secretName})
		return true
	}
	return false
}
