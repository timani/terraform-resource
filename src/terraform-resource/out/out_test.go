package out_test

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"

	"gopkg.in/yaml.v2"

	"terraform-resource/models"
	"terraform-resource/namer/namerfakes"
	"terraform-resource/out"
	"terraform-resource/storage"
	"terraform-resource/test/helpers"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var workingDir string

var _ = Describe("Out", func() {

	var (
		storageModel      storage.Model
		envName           string
		stateFilePath     string
		subnetCIDR        string
		workingDir        string
		fixtureEnvName    string
		pathToS3Fixture   string
		namer             namerfakes.FakeNamer
		assertOutBehavior func(models.OutRequest, map[string]string)
	)

	BeforeEach(func() {
		// TODO: avoid random clashes here
		rand.Seed(time.Now().UnixNano())
		subnetCIDR = fmt.Sprintf("10.0.%d.0/24", rand.Intn(256))

		envName = randomString("out-test")
		stateFilePath = path.Join(bucketPath, fmt.Sprintf("%s.tfstate", envName))

		storageModel = storage.Model{
			Bucket:          bucket,
			BucketPath:      bucketPath,
			AccessKeyID:     accessKey,
			SecretAccessKey: secretKey,
		}

		var err error
		workingDir, err = ioutil.TempDir(os.TempDir(), "terraform-resource-out-test")
		Expect(err).ToNot(HaveOccurred())

		// ensure relative paths resolve correctly
		err = os.Chdir(workingDir)
		Expect(err).ToNot(HaveOccurred())

		fixturesDir := path.Join(helpers.ProjectRoot(), "fixtures")
		err = exec.Command("cp", "-r", fixturesDir, workingDir).Run()
		Expect(err).ToNot(HaveOccurred())

		fixtureEnvName = randomString("s3-test-fixture")
		pathToS3Fixture = path.Join(bucketPath, fmt.Sprintf("%s.tfstate", fixtureEnvName))

		namer = namerfakes.FakeNamer{}
	})

	AfterEach(func() {
		_ = os.RemoveAll(workingDir)
		awsVerifier.DeleteSubnetWithCIDR(subnetCIDR, vpcID)
		awsVerifier.DeleteObjectFromS3(bucket, stateFilePath)
	})

	It("creates IaaS resources from a local terraform source", func() {
		req := models.OutRequest{
			Source: models.Source{
				Storage: storageModel,
			},
			Params: models.OutParams{
				EnvName: envName,
				Terraform: models.Terraform{
					Source: "fixtures/aws/",
					Vars: map[string]interface{}{
						"access_key":  accessKey,
						"secret_key":  secretKey,
						"vpc_id":      vpcID,
						"subnet_cidr": subnetCIDR,
					},
				},
			},
		}
		expectedMetadata := map[string]string{
			"vpc_id":      vpcID,
			"subnet_cidr": subnetCIDR,
			"tag_name":    "terraform-resource-test", // template default
		}

		assertOutBehavior(req, expectedMetadata)
	})

	It("creates IaaS resources from a remote terraform source", func() {
		req := models.OutRequest{
			Source: models.Source{
				Storage: storageModel,
			},
			Params: models.OutParams{
				EnvName: envName,
				Terraform: models.Terraform{
					// Note: changes to fixture must be pushed before running this test
					Source: "github.com/ljfranklin/terraform-resource//fixtures/aws/",
					Vars: map[string]interface{}{
						"access_key":  accessKey,
						"secret_key":  secretKey,
						"vpc_id":      vpcID,
						"subnet_cidr": subnetCIDR,
					},
				},
			},
		}
		expectedMetadata := map[string]string{
			"vpc_id":      vpcID,
			"subnet_cidr": subnetCIDR,
			"tag_name":    "terraform-resource-test", // template default
		}

		assertOutBehavior(req, expectedMetadata)
	})

	It("creates IaaS resources from a terraform module", func() {
		req := models.OutRequest{
			Source: models.Source{
				Storage: storageModel,
			},
			Params: models.OutParams{
				EnvName: envName,
				Terraform: models.Terraform{
					Source: "fixtures/module/",
					Vars: map[string]interface{}{
						"access_key":  accessKey,
						"secret_key":  secretKey,
						"vpc_id":      vpcID,
						"subnet_cidr": subnetCIDR,
					},
				},
			},
		}
		expectedMetadata := map[string]string{
			"vpc_id":      vpcID,
			"subnet_cidr": subnetCIDR,
			"tag_name":    "terraform-resource-module-test", // module default
		}

		assertOutBehavior(req, expectedMetadata)
	})

	It("creates IaaS resources from `source.terraform.vars`", func() {
		req := models.OutRequest{
			Source: models.Source{
				Storage: storageModel,
				Terraform: models.Terraform{
					Source: "fixtures/aws/",
					Vars: map[string]interface{}{
						"access_key":  accessKey,
						"secret_key":  secretKey,
						"vpc_id":      vpcID,
						"subnet_cidr": subnetCIDR,
						"tag_name":    "terraform-resource-source-test",
					},
				},
			},
			Params: models.OutParams{
				EnvName: envName,
			},
		}
		expectedMetadata := map[string]string{
			"vpc_id":      vpcID,
			"subnet_cidr": subnetCIDR,
			"tag_name":    "terraform-resource-source-test",
		}

		assertOutBehavior(req, expectedMetadata)
	})

	It("creates IaaS resources from `source.terraform` and `put.params.terraform`", func() {
		req := models.OutRequest{
			Source: models.Source{
				Storage: storageModel,
				Terraform: models.Terraform{
					Source: "fixtures/aws/",
					Vars: map[string]interface{}{
						"access_key": accessKey,
						"secret_key": "bad-secret-key", // will be overridden
					},
				},
			},
			// put params take precedence
			Params: models.OutParams{
				EnvName: envName,
				Terraform: models.Terraform{
					Vars: map[string]interface{}{
						"secret_key":  secretKey,
						"vpc_id":      vpcID,
						"subnet_cidr": subnetCIDR,
						"tag_name":    "terraform-resource-options-test",
					},
				},
			},
		}
		expectedMetadata := map[string]string{
			"vpc_id":      vpcID,
			"subnet_cidr": subnetCIDR,
			"tag_name":    "terraform-resource-options-test",
		}

		assertOutBehavior(req, expectedMetadata)
	})

	Context("when given a yaml file containing variables", func() {
		var varFileName string

		BeforeEach(func() {
			fileParams := map[string]interface{}{
				"vpc_id":   vpcID,
				"tag_name": "terraform-resource-file-test",
			}
			fileContent, err := yaml.Marshal(fileParams)
			Expect(err).ToNot(HaveOccurred())

			varFileName = fmt.Sprintf("%s.tf", randomString("tf-variables"))
			varFilePath := path.Join(workingDir, varFileName)
			varFile, err := os.Create(varFilePath)
			Expect(err).ToNot(HaveOccurred())
			defer varFile.Close()

			_, err = varFile.Write(fileContent)
			Expect(err).ToNot(HaveOccurred())
		})

		It("creates IaaS resources from request vars and file vars", func() {
			req := models.OutRequest{
				Source: models.Source{
					Storage: storageModel,
					Terraform: models.Terraform{
						Source: "fixtures/aws/",
						Vars: map[string]interface{}{
							"access_key": accessKey,
							// will be overridden
							"secret_key": "bad-secret-key",
						},
					},
				},
				// put params overrides source
				Params: models.OutParams{
					EnvName: envName,
					Terraform: models.Terraform{
						Vars: map[string]interface{}{
							"secret_key":  secretKey,
							"subnet_cidr": subnetCIDR,
							// will be overridden
							"tag_name": "terraform-resource-test-original",
						},
						// var file overrides put.params
						VarFile: varFileName,
					},
				},
			}
			expectedMetadata := map[string]string{
				"vpc_id":      vpcID,
				"subnet_cidr": subnetCIDR,
				"tag_name":    "terraform-resource-file-test",
			}

			assertOutBehavior(req, expectedMetadata)
		})
	})

	It("allows hashes and lists in metadata", func() {
		req := models.OutRequest{
			Source: models.Source{
				Storage: storageModel,
			},
			Params: models.OutParams{
				EnvName: envName,
				Terraform: models.Terraform{
					Source: "fixtures/aws/",
					Vars: map[string]interface{}{
						"access_key":  accessKey,
						"secret_key":  secretKey,
						"vpc_id":      vpcID,
						"subnet_cidr": subnetCIDR,
						"tag_name":    "terraform-resource-test-tags",
					},
				},
			},
		}
		expectedMetadata := map[string]string{
			"tag_hash": fmt.Sprintf(`{"EnvName":"%s","Name":"terraform-resource-test-tags"}`, envName),
			"tag_list": fmt.Sprintf(`["%s","terraform-resource-test-tags"]`, envName),
		}

		assertOutBehavior(req, expectedMetadata)
	})

	It("replaces spaces in env_name with hyphens", func() {
		spaceName := strings.Replace(envName, "-", " ", -1)
		req := models.OutRequest{
			Source: models.Source{
				Storage: storageModel,
			},
			Params: models.OutParams{
				EnvName: spaceName,
				Terraform: models.Terraform{
					Source: "fixtures/aws/",
					Vars: map[string]interface{}{
						"access_key":  accessKey,
						"secret_key":  secretKey,
						"vpc_id":      vpcID,
						"subnet_cidr": subnetCIDR,
					},
				},
			},
		}
		expectedMetadata := map[string]string{
			"vpc_id":      vpcID,
			"subnet_cidr": subnetCIDR,
			"tag_name":    "terraform-resource-test", // template default
		}

		assertOutBehavior(req, expectedMetadata)
	})

	It("trims whitespace from env_name", func() {
		spaceName := fmt.Sprintf(" %s \n", envName)
		req := models.OutRequest{
			Source: models.Source{
				Storage: storageModel,
			},
			Params: models.OutParams{
				EnvName: spaceName,
				Terraform: models.Terraform{
					Source: "fixtures/aws/",
					Vars: map[string]interface{}{
						"access_key":  accessKey,
						"secret_key":  secretKey,
						"vpc_id":      vpcID,
						"subnet_cidr": subnetCIDR,
					},
				},
			},
		}
		expectedMetadata := map[string]string{
			"vpc_id":      vpcID,
			"subnet_cidr": subnetCIDR,
			"tag_name":    "terraform-resource-test", // template default
		}

		assertOutBehavior(req, expectedMetadata)
	})

	It("automatically sets env_name as an input", func() {
		req := models.OutRequest{
			Source: models.Source{
				Storage: storageModel,
			},
			Params: models.OutParams{
				EnvName: envName,
				Terraform: models.Terraform{
					Source: "fixtures/aws/",
					Vars: map[string]interface{}{
						"access_key":  accessKey,
						"secret_key":  secretKey,
						"vpc_id":      vpcID,
						"subnet_cidr": subnetCIDR,
					},
				},
			},
		}
		expectedMetadata := map[string]string{
			"env_name": envName,
		}

		assertOutBehavior(req, expectedMetadata)
	})

	Context("when env_name_file is specified", func() {
		var (
			envNameFile string
		)

		BeforeEach(func() {
			tmpFile, err := ioutil.TempFile(workingDir, "env-file")
			Expect(err).ToNot(HaveOccurred())
			_, err = tmpFile.WriteString(envName)
			Expect(err).ToNot(HaveOccurred())
			envNameFile = tmpFile.Name()
		})

		AfterEach(func() {
			_ = os.RemoveAll(envNameFile)
		})

		It("Allows env name to be specified via env_name_file", func() {
			req := models.OutRequest{
				Source: models.Source{
					Storage: storageModel,
				},
				Params: models.OutParams{
					EnvNameFile: envNameFile,
					Terraform: models.Terraform{
						Source: "fixtures/aws/",
						Vars: map[string]interface{}{
							"access_key":  accessKey,
							"secret_key":  secretKey,
							"vpc_id":      vpcID,
							"subnet_cidr": subnetCIDR,
						},
					},
				},
			}
			expectedMetadata := map[string]string{
				"env_name": envName,
			}

			assertOutBehavior(req, expectedMetadata)
		})
	})

	It("creates an env with a random name when generate_random_name is true", func() {
		namer.RandomNameReturns(envName)

		req := models.OutRequest{
			Source: models.Source{
				Storage: storageModel,
			},
			Params: models.OutParams{
				GenerateRandomName: true,
				Terraform: models.Terraform{
					Source: "fixtures/aws/",
					Vars: map[string]interface{}{
						"access_key":  accessKey,
						"secret_key":  secretKey,
						"vpc_id":      vpcID,
						"subnet_cidr": subnetCIDR,
					},
				},
			},
		}
		expectedMetadata := map[string]string{
			"env_name": envName,
		}

		assertOutBehavior(req, expectedMetadata)

		Expect(namer.RandomNameCallCount()).To(Equal(1), "Expected RandomName to be called once")
	})

	It("encrypts the state file when server_side_encryption is given", func() {
		storageModel.ServerSideEncryption = "AES256"
		req := models.OutRequest{
			Source: models.Source{
				Storage: storageModel,
			},
			Params: models.OutParams{
				EnvName: envName,
				Terraform: models.Terraform{
					Source: "fixtures/aws/",
					Vars: map[string]interface{}{
						"access_key":  accessKey,
						"secret_key":  secretKey,
						"vpc_id":      vpcID,
						"subnet_cidr": subnetCIDR,
					},
				},
			},
		}
		expectedMetadata := map[string]string{
			"vpc_id":      vpcID,
			"subnet_cidr": subnetCIDR,
			"tag_name":    "terraform-resource-test", // template default
		}

		assertOutBehavior(req, expectedMetadata)

		awsVerifier.ExpectS3ServerSideEncryption(bucket, stateFilePath, "AES256")
	})

	It("encrypts the state file with a key ID when sse_kms_key_id is given", func() {
		if kmsKeyID == "" {
			Skip("S3_KMS_KEY_ID is not set, skipping sse_kms_key_id test...")
		}

		storageModel.SSEKMSKeyId = kmsKeyID
		req := models.OutRequest{
			Source: models.Source{
				Storage: storageModel,
			},
			Params: models.OutParams{
				EnvName: envName,
				Terraform: models.Terraform{
					Source: "fixtures/aws/",
					Vars: map[string]interface{}{
						"access_key":  accessKey,
						"secret_key":  secretKey,
						"vpc_id":      vpcID,
						"subnet_cidr": subnetCIDR,
					},
				},
			},
		}
		expectedMetadata := map[string]string{
			"vpc_id":      vpcID,
			"subnet_cidr": subnetCIDR,
			"tag_name":    "terraform-resource-test", // template default
		}

		assertOutBehavior(req, expectedMetadata)

		awsVerifier.ExpectS3ServerSideEncryption(bucket, stateFilePath, "aws:kms", kmsKeyID)
	})

	Context("when bucket contains a state file", func() {
		BeforeEach(func() {
			currFixture, err := os.Open(helpers.FileLocation("fixtures/s3/terraform-current.tfstate"))
			Expect(err).ToNot(HaveOccurred())
			defer currFixture.Close()
			awsVerifier.UploadObjectToS3(bucket, pathToS3Fixture, currFixture)
		})

		AfterEach(func() {
			awsVerifier.DeleteObjectFromS3(bucket, pathToS3Fixture)
		})

		It("returns an error if random name clashes", func() {
			// pick a name that always clashes
			namer.RandomNameReturns(fixtureEnvName)

			req := models.OutRequest{
				Source: models.Source{
					Storage: storageModel,
				},
				Params: models.OutParams{
					GenerateRandomName: true,
					Terraform: models.Terraform{
						Source: "fixtures/aws/",
						Vars: map[string]interface{}{
							"access_key":  accessKey,
							"secret_key":  secretKey,
							"vpc_id":      vpcID,
							"subnet_cidr": subnetCIDR,
						},
					},
				},
			}

			runner := out.Runner{
				SourceDir: workingDir,
				LogWriter: &bytes.Buffer{},
				Namer:     &namer,
			}
			_, err := runner.Run(req)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("random name"))
			Expect(namer.RandomNameCallCount()).To(Equal(out.NameClashRetries),
				"Expected RandomName to be called %d times", out.NameClashRetries)
		})
	})

	Context("given an invalid terraform var", func() {
		var req models.OutRequest
		BeforeEach(func() {
			req = models.OutRequest{
				Source: models.Source{
					Storage: storageModel,
				},
				Params: models.OutParams{
					EnvName: envName,
					Terraform: models.Terraform{
						Source: "fixtures/aws/",
						Vars: map[string]interface{}{
							"access_key":  accessKey,
							"secret_key":  secretKey,
							"vpc_id":      vpcID,
							"subnet_cidr": subnetCIDR,
							"acl_count":   "1",
							"acl_action":  "invalid-action",
						},
					},
				},
			}
		})

		It("does not delete partially created resources by default", func() {
			var logWriter bytes.Buffer
			runner := out.Runner{
				SourceDir: workingDir,
				LogWriter: &logWriter,
			}
			_, err := runner.Run(req)

			Expect(err).To(HaveOccurred())
			Expect(logWriter.String()).To(ContainSubstring("invalid-action"))
			awsVerifier.ExpectSubnetWithCIDRToExist(subnetCIDR, vpcID)

			originalStateFilePath := stateFilePath
			stateFilePath = path.Join(bucketPath, fmt.Sprintf("%s.tfstate.tainted", envName))
			awsVerifier.ExpectS3FileToNotExist(bucket, originalStateFilePath)
			awsVerifier.ExpectS3FileToExist(bucket, stateFilePath)

			// cleanup
			req.Params.Action = models.DestroyAction
			_, err = runner.Run(req)
			Expect(err).ToNot(HaveOccurred())
			awsVerifier.ExpectSubnetWithCIDRToNotExist(subnetCIDR, vpcID)
			awsVerifier.ExpectS3FileToNotExist(bucket, originalStateFilePath)
			awsVerifier.ExpectS3FileToNotExist(bucket, stateFilePath)
		})

		It("deletes all resources on failure if delete_on_failure is true", func() {
			req.Params.Terraform.DeleteOnFailure = true

			var logWriter bytes.Buffer
			runner := out.Runner{
				SourceDir: workingDir,
				LogWriter: &logWriter,
			}
			_, err := runner.Run(req)

			Expect(err).To(HaveOccurred())
			Expect(logWriter.String()).To(ContainSubstring("invalid-action"))
			awsVerifier.ExpectSubnetWithCIDRToNotExist(subnetCIDR, vpcID)

			originalStateFilePath := stateFilePath
			stateFilePath = path.Join(bucketPath, fmt.Sprintf("%s.tfstate.tainted", envName))
			awsVerifier.ExpectS3FileToNotExist(bucket, originalStateFilePath)
			awsVerifier.ExpectS3FileToNotExist(bucket, stateFilePath)
		})
	})

	Context("when an s3 compatible storage is used", func() {
		var s3Verifier *helpers.AWSVerifier

		BeforeEach(func() {
			storageModel = storage.Model{
				Endpoint:        s3CompatibleEndpoint,
				Bucket:          s3CompatibleBucket,
				BucketPath:      bucketPath,
				AccessKeyID:     s3CompatibleAccessKey,
				SecretAccessKey: s3CompatibleSecretKey,
			}

			s3Verifier = helpers.NewAWSVerifier(
				s3CompatibleAccessKey,
				s3CompatibleSecretKey,
				"",
				s3CompatibleEndpoint,
			)
		})

		AfterEach(func() {
			s3Verifier.DeleteObjectFromS3(s3CompatibleBucket, stateFilePath)
		})

		It("stores the state file successfully", func() {
			req := models.OutRequest{
				Source: models.Source{
					Storage: storageModel,
				},
				Params: models.OutParams{
					EnvName: envName,
					Terraform: models.Terraform{
						Source: "fixtures/aws/",
						Vars: map[string]interface{}{
							"access_key":  accessKey,
							"secret_key":  secretKey,
							"vpc_id":      vpcID,
							"subnet_cidr": subnetCIDR,
						},
					},
				},
			}
			expectedMetadata := map[string]string{
				"vpc_id":      vpcID,
				"subnet_cidr": subnetCIDR,
				"tag_name":    "terraform-resource-test", // template default
			}

			assertOutBehavior(req, expectedMetadata)

			s3Verifier.ExpectS3FileToExist(s3CompatibleBucket, stateFilePath)
		})
	})

	assertOutBehavior = func(outRequest models.OutRequest, expectedMetadata map[string]string) {
		var logWriter bytes.Buffer
		runner := out.Runner{
			SourceDir: workingDir,
			LogWriter: &logWriter,
			Namer:     &namer,
		}
		resp, err := runner.Run(outRequest)
		Expect(err).ToNot(HaveOccurred(), "Logs: %s", logWriter.String())

		Expect(logWriter.String()).To(ContainSubstring("Apply complete!"))

		Expect(resp.Version.EnvName).To(Equal(envName))

		Expect(resp.Metadata).ToNot(BeEmpty())
		fields := map[string]interface{}{}
		for _, field := range resp.Metadata {
			fields[field.Name] = field.Value
		}

		for key, value := range expectedMetadata {
			Expect(fields[key]).To(Equal(value))
		}

		Expect(fields["subnet_id"]).ToNot(BeEmpty())
		subnetID := fields["subnet_id"].(string)
		awsVerifier.ExpectSubnetToExist(subnetID)
		awsVerifier.ExpectSubnetToHaveTags(subnetID, map[string]string{
			"Name": fields["tag_name"].(string),
		})
	}
})

func randomString(prefix string) string {
	b := make([]byte, 4)
	_, err := rand.Read(b)
	Expect(err).ToNot(HaveOccurred())
	return fmt.Sprintf("%s-%x", prefix, b)
}
