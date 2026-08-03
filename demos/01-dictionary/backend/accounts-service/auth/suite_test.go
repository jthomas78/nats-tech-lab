package auth_test

import (
	"database/sql"
	"os/exec"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var (
	testDB          *sql.DB
	testUnavailable string
	testContainer   string
)

var _ = BeforeSuite(func() {
	db, containerID, err := startTestPostgres()
	if err != nil {
		testUnavailable = err.Error()
		return
	}
	testDB, testContainer = db, containerID
})

var _ = AfterSuite(func() {
	if testDB != nil {
		testDB.Close()
	}
	if testContainer != "" {
		_ = exec.Command("docker", "rm", "-f", testContainer).Run()
	}
})

func TestAuth(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Auth Suite")
}
