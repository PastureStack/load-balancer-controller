package rancher

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/rancher/lb-controller/config"
)

func newDirectoryTestController(t *testing.T) (*LoadBalancerController, *RCertificateFetcher, string, string) {
	t.Helper()

	testRoot := t.TempDir()
	copyTestTree(t, "testcerts", testRoot)
	certDir := filepath.Join(testRoot, "certs")
	defaultCertDir := filepath.Join(testRoot, "defaultCert")
	fetcher := &RCertificateFetcher{
		CertDir:        certDir,
		DefaultCertDir: defaultCertDir,
		CertName:       "fullchain.pem",
		KeyName:        "privkey.pem",
		CertsCache:     make(map[string]*config.Certificate),
		mu:             &sync.RWMutex{},
		initPollMu:     &sync.RWMutex{},
	}
	controller := &LoadBalancerController{
		stopCh:                     make(chan struct{}),
		incrementalBackoff:         0,
		incrementalBackoffInterval: 5,
		MetaFetcher:                tMetaFetcher{},
		LBProvider:                 &tProvider{},
		CertFetcher:                fetcher,
	}

	if !fetcher.pollCertificateDirectories(false) {
		t.Fatal("initial certificate directory poll did not populate the cache")
	}
	return controller, fetcher, certDir, defaultCertDir
}

func copyTestTree(t *testing.T, sourceRoot, destinationRoot string) {
	t.Helper()
	err := filepath.Walk(sourceRoot, func(sourcePath string, fileInfo os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relativePath, err := filepath.Rel(sourceRoot, sourcePath)
		if err != nil {
			return err
		}
		destinationPath := filepath.Join(destinationRoot, relativePath)
		if fileInfo.IsDir() {
			return os.MkdirAll(destinationPath, fileInfo.Mode())
		}
		content, err := ioutil.ReadFile(sourcePath)
		if err != nil {
			return err
		}
		return ioutil.WriteFile(destinationPath, content, fileInfo.Mode())
	})
	if err != nil {
		t.Fatalf("failed to copy certificate fixtures: %v", err)
	}
}

func TestReadCertDirs(t *testing.T) {
	controller, _, _, _ := newDirectoryTestController(t)
	configs, err := controller.BuildConfigFromMetadata("test", "", "", "any", nil)
	if err != nil {
		t.Fatalf("error building config: %v", err)
	}
	if len(configs[0].Certs) != 3 {
		t.Fatalf("expected 3 certificates, got %d", len(configs[0].Certs))
	}
}

func TestReadDefaultCertDir(t *testing.T) {
	controller, _, _, _ := newDirectoryTestController(t)
	configs, err := controller.BuildConfigFromMetadata("test", "", "", "any", nil)
	if err != nil {
		t.Fatalf("error building config: %v", err)
	}
	if configs[0].DefaultCert == nil {
		t.Fatal("default certificate was not loaded")
	}
}

func TestCheckCertDirUpdate(t *testing.T) {
	controller, fetcher, certDir, _ := newDirectoryTestController(t)
	newCertDir := filepath.Join(certDir, "c.com")
	if err := os.MkdirAll(newCertDir, 0700); err != nil {
		t.Fatalf("failed to create certificate directory: %v", err)
	}
	if err := ioutil.WriteFile(filepath.Join(newCertDir, "fullchain.pem"), []byte("certificate\n"), 0600); err != nil {
		t.Fatalf("failed to write certificate: %v", err)
	}

	fetcher.pollCertificateDirectories(false)
	configs, err := controller.BuildConfigFromMetadata("test", "", "", "any", nil)
	if err != nil {
		t.Fatalf("error building config: %v", err)
	}
	if len(configs[0].Certs) != 3 {
		t.Fatalf("incomplete certificate should be ignored; got %d certificates", len(configs[0].Certs))
	}

	if err := ioutil.WriteFile(filepath.Join(newCertDir, "privkey.pem"), []byte("private key\n"), 0600); err != nil {
		t.Fatalf("failed to write private key: %v", err)
	}
	if !fetcher.pollCertificateDirectories(false) {
		t.Fatal("complete certificate update was not detected")
	}
	configs, err = controller.BuildConfigFromMetadata("test", "", "", "any", nil)
	if err != nil {
		t.Fatalf("error building updated config: %v", err)
	}
	if len(configs[0].Certs) != 4 {
		t.Fatalf("expected 4 certificates after update, got %d", len(configs[0].Certs))
	}
}

func TestCheckDefaultCertRemoval(t *testing.T) {
	controller, fetcher, _, defaultCertDir := newDirectoryTestController(t)
	certificatePath := filepath.Join(defaultCertDir, "default.com", "fullchain.pem")
	temporaryPath := filepath.Join(t.TempDir(), "fullchain.pem")
	if err := os.Rename(certificatePath, temporaryPath); err != nil {
		t.Fatalf("failed to move default certificate: %v", err)
	}

	if !fetcher.pollCertificateDirectories(false) {
		t.Fatal("default certificate removal was not detected")
	}
	configs, err := controller.BuildConfigFromMetadata("test", "", "", "any", nil)
	if err != nil {
		t.Fatalf("error building config after removal: %v", err)
	}
	if configs[0].DefaultCert != nil {
		t.Fatal("removed default certificate remained in the cache")
	}

	if err := os.Rename(temporaryPath, certificatePath); err != nil {
		t.Fatalf("failed to restore default certificate: %v", err)
	}
	if !fetcher.pollCertificateDirectories(false) {
		t.Fatal("restored default certificate was not detected")
	}
	configs, err = controller.BuildConfigFromMetadata("test", "", "", "any", nil)
	if err != nil {
		t.Fatalf("error building config after restore: %v", err)
	}
	if configs[0].DefaultCert == nil {
		t.Fatal("restored default certificate was not loaded")
	}
}

func TestEvaluateLinkAndReadFileAllowsInRootSymlink(t *testing.T) {
	certDir := filepath.Join(t.TempDir(), "certs")
	certificateDir := filepath.Join(certDir, "service")
	sharedDir := filepath.Join(certDir, "shared")
	if err := os.MkdirAll(certificateDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sharedDir, 0700); err != nil {
		t.Fatal(err)
	}
	targetPath := filepath.Join(sharedDir, "fullchain.pem")
	if err := ioutil.WriteFile(targetPath, []byte("trusted certificate\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..", "shared", "fullchain.pem"), filepath.Join(certificateDir, "fullchain.pem")); err != nil {
		t.Fatal(err)
	}

	fetcher := &RCertificateFetcher{CertDir: certDir}
	content, err := fetcher.evaluatueLinkAndReadFile(certificateDir, "fullchain.pem")
	if err != nil {
		t.Fatalf("in-root symlink was rejected: %v", err)
	}
	if string(*content) != "trusted certificate\n" {
		t.Fatalf("unexpected certificate content: %q", string(*content))
	}
}

func TestEvaluateLinkAndReadFileRejectsEscapingSymlink(t *testing.T) {
	testRoot := t.TempDir()
	certDir := filepath.Join(testRoot, "certs")
	certificateDir := filepath.Join(certDir, "service")
	if err := os.MkdirAll(certificateDir, 0700); err != nil {
		t.Fatal(err)
	}
	secretPath := filepath.Join(testRoot, "outside-secret")
	if err := ioutil.WriteFile(secretPath, []byte("must not be read\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..", "..", "outside-secret"), filepath.Join(certificateDir, "fullchain.pem")); err != nil {
		t.Fatal(err)
	}

	fetcher := &RCertificateFetcher{CertDir: certDir}
	if _, err := fetcher.evaluatueLinkAndReadFile(certificateDir, "fullchain.pem"); err == nil {
		t.Fatal("certificate symlink escaping the configured directory was accepted")
	}
}

func TestEvaluateLinkAndReadFileRejectsEscapingSymlinkChain(t *testing.T) {
	testRoot := t.TempDir()
	certDir := filepath.Join(testRoot, "certs")
	certificateDir := filepath.Join(certDir, "service")
	if err := os.MkdirAll(certificateDir, 0700); err != nil {
		t.Fatal(err)
	}
	secretPath := filepath.Join(testRoot, "outside-secret")
	if err := ioutil.WriteFile(secretPath, []byte("must not be read\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("intermediate.pem", filepath.Join(certificateDir, "fullchain.pem")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("..", "..", "outside-secret"), filepath.Join(certificateDir, "intermediate.pem")); err != nil {
		t.Fatal(err)
	}

	fetcher := &RCertificateFetcher{CertDir: certDir}
	if _, err := fetcher.evaluatueLinkAndReadFile(certificateDir, "fullchain.pem"); err == nil {
		t.Fatal("certificate symlink chain escaping the configured directory was accepted")
	}
}

func TestEvaluateLinkAndReadFileRejectsEscapingParentSymlink(t *testing.T) {
	testRoot := t.TempDir()
	certDir := filepath.Join(testRoot, "certs")
	outsideDir := filepath.Join(testRoot, "outside")
	if err := os.MkdirAll(certDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outsideDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := ioutil.WriteFile(filepath.Join(outsideDir, "fullchain.pem"), []byte("must not be read\n"), 0600); err != nil {
		t.Fatal(err)
	}
	linkedDirectory := filepath.Join(certDir, "service")
	if err := os.Symlink(outsideDir, linkedDirectory); err != nil {
		t.Fatal(err)
	}

	fetcher := &RCertificateFetcher{CertDir: certDir}
	if _, err := fetcher.evaluatueLinkAndReadFile(linkedDirectory, "fullchain.pem"); err == nil {
		t.Fatal("regular certificate below an escaping parent symlink was accepted")
	}
}

func TestEvaluateLinkAndReadFileAllowsConfiguredRootSymlink(t *testing.T) {
	testRoot := t.TempDir()
	realCertDir := filepath.Join(testRoot, "real-certs")
	certificateDir := filepath.Join(realCertDir, "service")
	if err := os.MkdirAll(certificateDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := ioutil.WriteFile(filepath.Join(certificateDir, "fullchain.pem"), []byte("trusted certificate\n"), 0600); err != nil {
		t.Fatal(err)
	}
	configuredCertDir := filepath.Join(testRoot, "mounted-certs")
	if err := os.Symlink(realCertDir, configuredCertDir); err != nil {
		t.Fatal(err)
	}

	fetcher := &RCertificateFetcher{CertDir: configuredCertDir}
	content, err := fetcher.evaluatueLinkAndReadFile(filepath.Join(configuredCertDir, "service"), "fullchain.pem")
	if err != nil {
		t.Fatalf("configured certificate root symlink was rejected: %v", err)
	}
	if string(*content) != "trusted certificate\n" {
		t.Fatalf("unexpected certificate content: %q", string(*content))
	}
}
