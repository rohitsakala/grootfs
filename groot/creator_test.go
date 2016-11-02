package groot_test

import (
	"errors"
	"io/ioutil"
	"net/url"
	"os"

	"code.cloudfoundry.org/grootfs/groot"
	"code.cloudfoundry.org/grootfs/groot/grootfakes"
	"code.cloudfoundry.org/lager"
	"code.cloudfoundry.org/lager/lagertest"
	specsv1 "github.com/opencontainers/image-spec/specs-go/v1"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Creator", func() {
	var (
		fakeBundler           *grootfakes.FakeBundler
		fakeImagePuller       *grootfakes.FakeImagePuller
		fakeLocksmith         *grootfakes.FakeLocksmith
		fakeDependencyManager *grootfakes.FakeDependencyManager
		lockFile              *os.File

		creator *groot.Creator
		logger  lager.Logger
	)

	BeforeEach(func() {
		var err error

		fakeBundler = new(grootfakes.FakeBundler)
		fakeImagePuller = new(grootfakes.FakeImagePuller)

		fakeLocksmith = new(grootfakes.FakeLocksmith)
		lockFile, err = ioutil.TempFile("", "")
		Expect(err).NotTo(HaveOccurred())
		fakeLocksmith.LockReturns(lockFile, nil)
		fakeDependencyManager = new(grootfakes.FakeDependencyManager)

		creator = groot.IamCreator(fakeBundler, fakeImagePuller, fakeLocksmith, fakeDependencyManager)
		logger = lagertest.NewTestLogger("creator")
	})

	AfterEach(func() {
		Expect(os.Remove(lockFile.Name())).To(Succeed())
	})

	Describe("Create", func() {
		It("acquires the global lock", func() {
			_, err := creator.Create(logger, groot.CreateSpec{
				Image: "/path/to/image",
			})
			Expect(err).NotTo(HaveOccurred())

			Expect(fakeLocksmith.LockCallCount()).To(Equal(1))
			Expect(fakeLocksmith.LockArgsForCall(0)).To(Equal(groot.GLOBAL_LOCK_KEY))
		})

		It("pulls the image", func() {
			uidMappings := []groot.IDMappingSpec{groot.IDMappingSpec{HostID: 1, NamespaceID: 2, Size: 10}}
			gidMappings := []groot.IDMappingSpec{groot.IDMappingSpec{HostID: 10, NamespaceID: 20, Size: 100}}

			_, err := creator.Create(logger, groot.CreateSpec{
				Image:       "/path/to/image",
				UIDMappings: uidMappings,
				GIDMappings: gidMappings,
			})
			Expect(err).NotTo(HaveOccurred())

			imageURL, err := url.Parse("/path/to/image")
			Expect(err).NotTo(HaveOccurred())
			_, imageSpec := fakeImagePuller.PullArgsForCall(0)
			Expect(imageSpec.ImageSrc).To(Equal(imageURL))
			Expect(imageSpec.UIDMappings).To(Equal(uidMappings))
			Expect(imageSpec.GIDMappings).To(Equal(gidMappings))
		})

		It("makes a bundle", func() {
			image := groot.Image{
				VolumePath: "/path/to/volume",
				Image: specsv1.Image{
					Author: "Groot",
				},
			}
			fakeImagePuller.PullReturns(image, nil)

			_, err := creator.Create(logger, groot.CreateSpec{
				ID:    "some-id",
				Image: "/path/to/image",
			})
			Expect(err).NotTo(HaveOccurred())

			Expect(fakeBundler.CreateCallCount()).To(Equal(1))
			_, createBundlerSpec := fakeBundler.CreateArgsForCall(0)
			Expect(createBundlerSpec).To(Equal(groot.BundleSpec{
				ID:         "some-id",
				VolumePath: "/path/to/volume",
				Image: specsv1.Image{
					Author: "Groot",
				},
			}))
		})

		It("registers chain ids used by a bundle", func() {
			image := groot.Image{
				ChainIDs: []string{"sha256:vol-a", "sha256:vol-b"},
			}
			fakeImagePuller.PullReturns(image, nil)

			_, err := creator.Create(logger, groot.CreateSpec{
				ID:    "my-bundle",
				Image: "/path/to/image",
			})

			Expect(err).NotTo(HaveOccurred())

			bundleID, chainIDs := fakeDependencyManager.RegisterArgsForCall(0)
			Expect(bundleID).To(Equal("bundle:my-bundle"))
			Expect(chainIDs).To(Equal([]string{"sha256:vol-a", "sha256:vol-b"}))
		})

		It("registers image name with chain ids used by a bundle", func() {
			image := groot.Image{
				ChainIDs: []string{"sha256:vol-a", "sha256:vol-b"},
			}
			fakeImagePuller.PullReturns(image, nil)

			_, err := creator.Create(logger, groot.CreateSpec{
				ID:    "my-bundle",
				Image: "docker:///ubuntu",
			})

			Expect(err).NotTo(HaveOccurred())

			Expect(fakeDependencyManager.RegisterCallCount()).To(Equal(2))
			imageName, chainIDs := fakeDependencyManager.RegisterArgsForCall(1)
			Expect(imageName).To(Equal("image:ubuntu"))
			Expect(chainIDs).To(Equal([]string{"sha256:vol-a", "sha256:vol-b"}))
		})

		It("releases the global lock", func() {
			_, err := creator.Create(logger, groot.CreateSpec{
				Image: "/path/to/image",
			})
			Expect(err).NotTo(HaveOccurred())

			Expect(fakeLocksmith.UnlockCallCount()).To(Equal(1))
			Expect(fakeLocksmith.UnlockArgsForCall(0)).To(Equal(lockFile))
		})

		It("returns the bundle", func() {
			fakeBundler.CreateReturns(groot.Bundle{
				Path: "/path/to/bundle",
			}, nil)

			bundle, err := creator.Create(logger, groot.CreateSpec{})
			Expect(err).NotTo(HaveOccurred())
			Expect(bundle.Path).To(Equal("/path/to/bundle"))
		})

		Context("when the image has a tag", func() {
			It("registers image name with chain ids used by a bundle", func() {
				image := groot.Image{
					ChainIDs: []string{"sha256:vol-a", "sha256:vol-b"},
				}
				fakeImagePuller.PullReturns(image, nil)

				_, err := creator.Create(logger, groot.CreateSpec{
					ID:    "my-bundle",
					Image: "docker:///ubuntu:latest",
				})

				Expect(err).NotTo(HaveOccurred())

				Expect(fakeDependencyManager.RegisterCallCount()).To(Equal(2))
				imageName, chainIDs := fakeDependencyManager.RegisterArgsForCall(1)
				Expect(imageName).To(Equal("image:ubuntu:latest"))
				Expect(chainIDs).To(Equal([]string{"sha256:vol-a", "sha256:vol-b"}))
			})
		})

		Context("when the image is not a valid URL", func() {
			It("returns an error", func() {
				_, err := creator.Create(logger, groot.CreateSpec{
					Image: "%%!!#@!^&",
				})
				Expect(err).To(MatchError(ContainSubstring("parsing image url")))
			})

			It("does not create a bundle", func() {
				_, err := creator.Create(logger, groot.CreateSpec{
					Image: "%%!!#@!^&",
				})
				Expect(err).To(HaveOccurred())

				Expect(fakeBundler.CreateCallCount()).To(Equal(0))
			})
		})

		Context("when the id already exists", func() {
			BeforeEach(func() {
				fakeBundler.ExistsReturns(true, nil)
			})

			It("returns an error", func() {
				_, err := creator.Create(logger, groot.CreateSpec{
					Image: "/path/to/image",
					ID:    "some-id",
				})
				Expect(err).To(HaveOccurred())

				Expect(fakeBundler.CreateCallCount()).To(Equal(0))
				Expect(err).To(MatchError(ContainSubstring("bundle for id `some-id` already exists")))
			})

			It("does not pull the image", func() {
				_, err := creator.Create(logger, groot.CreateSpec{
					Image: "/path/to/image",
					ID:    "some-id",
				})
				Expect(err).To(HaveOccurred())

				Expect(fakeImagePuller.PullCallCount()).To(Equal(0))
				Expect(err).To(MatchError(ContainSubstring("bundle for id `some-id` already exists")))
			})
		})

		Context("when checking if the id exists fails", func() {
			BeforeEach(func() {
				fakeBundler.ExistsReturns(false, errors.New("Checking if the bundle ID exists"))
			})

			It("returns an error", func() {
				_, err := creator.Create(logger, groot.CreateSpec{
					Image: "/path/to/image",
				})
				Expect(err).To(HaveOccurred())

				Expect(fakeBundler.CreateCallCount()).To(Equal(0))
				Expect(err).To(MatchError(ContainSubstring("Checking if the bundle ID exists")))
			})

			It("does not pull the image", func() {
				_, err := creator.Create(logger, groot.CreateSpec{
					Image: "/path/to/image",
				})
				Expect(err).To(HaveOccurred())

				Expect(fakeImagePuller.PullCallCount()).To(Equal(0))
				Expect(err).To(MatchError(ContainSubstring("Checking if the bundle ID exists")))
			})
		})

		Context("when acquiring the lock fails", func() {
			BeforeEach(func() {
				fakeLocksmith.LockReturns(nil, errors.New("failed to lock"))
			})

			It("returns the error", func() {
				_, err := creator.Create(logger, groot.CreateSpec{
					Image: "/path/to/image",
				})
				Expect(err).To(MatchError(ContainSubstring("failed to lock")))
			})

			It("does not pull the image", func() {
				_, err := creator.Create(logger, groot.CreateSpec{
					Image: "/path/to/image",
				})
				Expect(err).To(HaveOccurred())

				Expect(fakeImagePuller.PullCallCount()).To(BeZero())
			})
		})

		Context("when pulling the image fails", func() {
			BeforeEach(func() {
				fakeImagePuller.PullReturns(groot.Image{}, errors.New("failed to pull image"))
			})

			It("returns the error", func() {
				_, err := creator.Create(logger, groot.CreateSpec{
					Image: "/path/to/image",
				})
				Expect(err).To(MatchError(ContainSubstring("failed to pull image")))
			})

			It("does not create a bundle", func() {
				_, err := creator.Create(logger, groot.CreateSpec{
					Image: "/path/to/image",
				})
				Expect(err).To(HaveOccurred())

				Expect(fakeBundler.CreateCallCount()).To(Equal(0))
			})
		})

		Context("when creating the bundle fails", func() {
			BeforeEach(func() {
				fakeBundler.CreateReturns(groot.Bundle{}, errors.New("Failed to make bundle"))
			})

			It("returns the error", func() {
				_, err := creator.Create(logger, groot.CreateSpec{})
				Expect(err).To(MatchError("making bundle: Failed to make bundle"))
			})
		})

		Context("when registering dependencies fails", func() {
			BeforeEach(func() {
				fakeDependencyManager.RegisterReturns(errors.New("failed to register dependencies"))
				fakeImagePuller.PullReturns(groot.Image{}, nil)
			})

			It("returns an errors", func() {
				_, err := creator.Create(logger, groot.CreateSpec{
					ID:    "my-bundle",
					Image: "/path/to/image",
				})

				Expect(err).To(MatchError(ContainSubstring("failed to register dependencies")))
			})

			It("destroys the bundle", func() {
				_, err := creator.Create(logger, groot.CreateSpec{
					ID:    "my-bundle",
					Image: "/path/to/image",
				})

				Expect(err).To(HaveOccurred())
				Expect(fakeBundler.DestroyCallCount()).To(Equal(1))
			})
		})

		Context("when disk limit is given", func() {
			It("passes the disk limit to the bundler", func() {
				image := groot.Image{
					VolumePath: "/path/to/volume",
					Image: specsv1.Image{
						Author: "Groot",
					},
				}
				fakeImagePuller.PullReturns(image, nil)

				_, err := creator.Create(logger, groot.CreateSpec{
					ID:        "some-id",
					DiskLimit: int64(1024),
					Image:     "/path/to/image",
				})
				Expect(err).NotTo(HaveOccurred())

				Expect(fakeBundler.CreateCallCount()).To(Equal(1))
				_, createBundlerSpec := fakeBundler.CreateArgsForCall(0)
				Expect(createBundlerSpec).To(Equal(groot.BundleSpec{
					ID:         "some-id",
					VolumePath: "/path/to/volume",
					Image: specsv1.Image{
						Author: "Groot",
					},
					DiskLimit: int64(1024),
				}))
			})
		})
	})
})