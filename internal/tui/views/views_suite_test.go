package views

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestViews(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Views Suite")
}
