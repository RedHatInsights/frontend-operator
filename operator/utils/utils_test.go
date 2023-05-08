package utils

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestCamelCase(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Camel Case Conversion")

}

var _ = Describe("Camel Case Conversion", func() {
	const (
		singleName      = "inventory"
		dashName        = "hac-core"
		camelName       = "hacCore"
		tripleDash      = "frontend-test-name"
		tripleCamelHump = "frontendTestName"
	)

	Context("When creating a fed-modules entry", func() {
		It("Should convert a dash separated name", func() {
			By("Using the ToCamelCase method")
			Expect(ToCamelCase(dashName)).Should(Equal(camelName))
		})
	})

	Context("When creating a fed-modules entry", func() {
		It("Should convert dash case to camel case for n dashes", func() {
			By("Using the ToCamelCase method")
			Expect(ToCamelCase(tripleDash)).Should(Equal(tripleCamelHump))
		})
	})

	Context("When creating a fed-modules entry", func() {
		It("Should not convert a single word", func() {
			By("Using the ToCamelCase method")
			Expect(ToCamelCase(singleName)).Should(Equal(singleName))
		})
	})

})
