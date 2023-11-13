package utils

import (
	"testing"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
)

func TestCamelCase(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Camel Case Conversion")

}

var _ = ginkgo.Describe("Camel Case Conversion", func() {
	const (
		singleName      = "inventory"
		dashName        = "hac-core"
		camelName       = "hacCore"
		tripleDash      = "frontend-test-name"
		tripleCamelHump = "frontendTestName"
	)

	ginkgo.Context("When creating a fed-modules entry", func() {
		ginkgo.It("Should convert a dash separated name", func() {
			ginkgo.By("Using the ToCamelCase method")
			gomega.Expect(ToCamelCase(dashName)).Should(gomega.Equal(camelName))
		})
	})

	ginkgo.Context("When creating a fed-modules entry", func() {
		ginkgo.It("Should convert dash case to camel case for n dashes", func() {
			ginkgo.By("Using the ToCamelCase method")
			gomega.Expect(ToCamelCase(tripleDash)).Should(gomega.Equal(tripleCamelHump))
		})
	})

	ginkgo.Context("When creating a fed-modules entry", func() {
		ginkgo.It("Should not convert a single word", func() {
			ginkgo.By("Using the ToCamelCase method")
			gomega.Expect(ToCamelCase(singleName)).Should(gomega.Equal(singleName))
		})
	})

})
