package connect

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestConnect(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Connect Suite")
}
