package remote

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"

	"code.cloudfoundry.org/lager"
	"github.com/Sirupsen/logrus"
	"github.com/containers/image/docker"
	manifestpkg "github.com/containers/image/manifest"
	"github.com/containers/image/types"
	specsv1 "github.com/opencontainers/image-spec/specs-go/v1"
)

type DockerSource struct {
	trustedRegistries []string
}

func NewDockerSource(trustedRegistries []string) *DockerSource {
	return &DockerSource{
		trustedRegistries: trustedRegistries,
	}
}

func (s *DockerSource) Manifest(logger lager.Logger, imageURL *url.URL) (Manifest, error) {
	logger = logger.Session("fetching-image-manifest", lager.Data{"imageURL": imageURL})
	logger.Info("start")
	defer logger.Info("end")

	img, err := s.preSteamedImage(logger, imageURL)
	if err != nil {
		return Manifest{}, err
	}

	contents, mimeType, err := img.Manifest()
	if err != nil {
		if strings.Contains(err.Error(), "error fetching manifest: status code:") {
			logger.Error("fetching-manifest-failed", err)
			return Manifest{}, fmt.Errorf("image does not exist or you do not have permissions to see it: %s", err)
		}

		if strings.Contains(err.Error(), "malformed HTTP response") {
			logger.Error("fetching-manifest-failed", err)
			return Manifest{}, fmt.Errorf("TLS validation of insecure registry failed: %s", err)
		}

		return Manifest{}, err
	}

	var manifest Manifest
	switch mimeType {
	case manifestpkg.DockerV2Schema1MediaType, manifestpkg.DockerV2Schema1SignedMediaType:
		logger.Debug("docker-image-version-2-schema-1")
		manifest, err = s.parseSchemaV1Manifest(logger, contents)

	case specsv1.MediaTypeImageManifest, manifestpkg.DockerV2Schema2MediaType:
		logger.Debug("docker-image-version-2-schema-2")
		manifest, err = s.parseSchemaV2Manifest(logger, contents)

	default:
		return Manifest{}, errors.New(fmt.Sprintf("unknown media type '%s'", mimeType))
	}

	return manifest, nil
}

func (s *DockerSource) Config(logger lager.Logger, imageURL *url.URL, manifest Manifest) (specsv1.Image, error) {
	logger = logger.Session("fetching-image-config", lager.Data{
		"imageURL":     imageURL,
		"configDigest": manifest.ConfigCacheKey,
	})
	logger.Info("start")
	defer logger.Info("end")

	var (
		config specsv1.Image
		err    error
	)

	switch manifest.SchemaVersion {
	case 1:
		logger.Debug("docker-image-version-2-schema-1")
		config, err = s.parseSchemaV1Config(logger, manifest)
		if err != nil {
			return specsv1.Image{}, err
		}
	case 2:
		logger.Debug("docker-image-version-2-schema-2")
		config, err = s.parseSchemaV2Config(logger, imageURL, manifest.ConfigCacheKey)
		if err != nil {
			return specsv1.Image{}, err
		}
	default:
		return specsv1.Image{}, fmt.Errorf("schema version not supported (%d)", manifest.SchemaVersion)
	}

	return config, nil
}

func (s *DockerSource) StreamBlob(logger lager.Logger, imageURL *url.URL, digest string) (io.ReadCloser, int64, error) {
	logrus.SetOutput(os.Stderr)
	logger = logger.Session("streaming-blob", lager.Data{
		"imageURL": imageURL,
		"digest":   digest,
	})
	logger.Info("start")
	defer logger.Info("end")

	imgSrc, err := s.preSteamedImageSource(logger, imageURL)
	if err != nil {
		return nil, 0, err
	}

	stream, size, err := imgSrc.GetBlob(digest)
	if err != nil {
		return nil, 0, err
	}
	logger.Debug("got-blob-stream", lager.Data{"size": size})

	tarStream, err := gzip.NewReader(stream)
	if err != nil {
		return nil, 0, fmt.Errorf("reading gzip: %s", err)
	}

	return tarStream, 0, nil
}

func (s *DockerSource) skipTLSValidation(imageURL *url.URL) bool {
	for _, trustedRegistry := range s.trustedRegistries {
		if imageURL.Host == trustedRegistry {
			return true
		}
	}

	return false
}

func (s *DockerSource) parseSchemaV1Manifest(logger lager.Logger, rawManifest []byte) (Manifest, error) {
	var dockerManifest SchemaV1Manifest
	if err := json.Unmarshal(rawManifest, &dockerManifest); err != nil {
		logger.Error("parsing-manifest-failed", err, lager.Data{"manifest": string(rawManifest)})
		return Manifest{}, fmt.Errorf("parsing manifest: %s", err)
	}

	manifest := Manifest{}
	for _, layer := range dockerManifest.FSLayers {
		manifest.Layers = append([]string{layer["blobSum"]}, manifest.Layers...)
	}

	for _, history := range dockerManifest.History {
		manifest.V1Compatibility = append([]string{history.V1Compatibility}, manifest.V1Compatibility...)
	}

	v1Config := manifest.V1Compatibility[len(manifest.V1Compatibility)-1]
	configSha := sha256.Sum256([]byte(v1Config))
	manifest.ConfigCacheKey = fmt.Sprintf("sha256:%s", hex.EncodeToString(configSha[:32]))
	manifest.SchemaVersion = 1

	return manifest, nil
}

func (s *DockerSource) parseSchemaV2Manifest(logger lager.Logger, rawManifest []byte) (Manifest, error) {
	var ociManifest specsv1.Manifest
	if err := json.Unmarshal(rawManifest, &ociManifest); err != nil {
		logger.Error("parsing-manifest-failed", err, lager.Data{"manifest": string(rawManifest)})
		return Manifest{}, fmt.Errorf("parsing manifest: %s", err)
	}

	manifest := Manifest{
		ConfigCacheKey: ociManifest.Config.Digest,
	}
	for _, layer := range ociManifest.Layers {
		manifest.Layers = append(manifest.Layers, layer.Digest)
	}

	manifest.SchemaVersion = 2
	return manifest, nil
}

func (s *DockerSource) parseSchemaV2Config(logger lager.Logger, imageURL *url.URL, configDigest string) (specsv1.Image, error) {
	imgSrc, err := s.preSteamedImageSource(logger, imageURL)
	if err != nil {
		return specsv1.Image{}, err
	}

	stream, _, err := imgSrc.GetBlob(configDigest)
	if err != nil {
		if strings.Contains(err.Error(), "malformed HTTP response") {
			logger.Error("fetching-config-failed", err)
			return specsv1.Image{}, fmt.Errorf("TLS validation of insecure registry failed: %s", err)
		}
		return specsv1.Image{}, fmt.Errorf("fetching config blob: %s", err)
	}

	var config specsv1.Image
	if err := json.NewDecoder(stream).Decode(&config); err != nil {
		logger.Error("parsing-config-failed", err)
		return specsv1.Image{}, fmt.Errorf("parsing image config: %s", err)
	}

	return config, nil
}

func (s *DockerSource) parseSchemaV1Config(logger lager.Logger, manifest Manifest) (specsv1.Image, error) {
	if len(manifest.V1Compatibility) == 0 {
		logger.Error("v1-manifest-validation-failed", errors.New("v1compatibility has no layers"), lager.Data{"manifest": manifest})
		return specsv1.Image{}, errors.New("V1Compatibility is empty for the manifest")
	}

	var config specsv1.Image
	v1Config := manifest.V1Compatibility[len(manifest.V1Compatibility)-1]
	if err := json.Unmarshal([]byte(v1Config), &config); err != nil {
		logger.Error("parsing-manifest-v1-compatibility-failed", err)
		return specsv1.Image{}, fmt.Errorf("parsing manifest V1Compatibility: %s", err)
	}

	for _, rawHistory := range manifest.V1Compatibility {
		var v1Compatibility V1Compatibility
		if err := json.Unmarshal([]byte(rawHistory), &v1Compatibility); err != nil {
			logger.Error("parsing-manifest-v1-compatibility-failed", err)
			return specsv1.Image{}, fmt.Errorf("parsing manifest V1Compatibility: %s", err)
		}
		config.RootFS.DiffIDs = append(config.RootFS.DiffIDs, "sha256:"+v1Compatibility.ID)
	}

	return config, nil
}

func (s *DockerSource) preSteamedReference(logger lager.Logger, imageURL *url.URL) (types.ImageReference, error) {
	refString := "/"
	if imageURL.Host != "" {
		refString += "/" + imageURL.Host
	}
	refString += imageURL.Path

	logger.Debug("parsing-reference", lager.Data{"refString": refString})
	ref, err := docker.ParseReference(refString)
	if err != nil {
		return nil, fmt.Errorf("parsing url failed: %s", err)
	}

	return ref, nil
}

func (s *DockerSource) preSteamedImage(logger lager.Logger, imageURL *url.URL) (types.Image, error) {
	ref, err := s.preSteamedReference(logger, imageURL)
	if err != nil {
		return nil, err
	}

	skipTLSValidation := s.skipTLSValidation(imageURL)
	logger.Debug("new-image", lager.Data{"skipTLSValidation": skipTLSValidation})
	img, err := ref.NewImage(&types.SystemContext{DockerInsecureSkipTLSVerify: skipTLSValidation})
	if err != nil {
		return nil, fmt.Errorf("creating reference: %s", err)
	}

	return img, nil
}

func (s *DockerSource) preSteamedImageSource(logger lager.Logger, imageURL *url.URL) (types.ImageSource, error) {
	ref, err := s.preSteamedReference(logger, imageURL)
	if err != nil {
		return nil, err
	}

	skipTLSValidation := s.skipTLSValidation(imageURL)

	imgSrc, _ := ref.NewImageSource(&types.SystemContext{DockerInsecureSkipTLSVerify: skipTLSValidation}, preferedMediaTypes())
	if err != nil {
		return nil, fmt.Errorf("creating reference: %s", err)
	}
	logger.Debug("new-image", lager.Data{"skipTLSValidation": skipTLSValidation})

	return imgSrc, nil
}

func preferedMediaTypes() []string {
	return []string{
		specsv1.MediaTypeImageManifest,
		manifestpkg.DockerV2Schema2MediaType,
	}
}