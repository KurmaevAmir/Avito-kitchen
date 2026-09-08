package httpapi

import (
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/gin-gonic/gin"
	ginmiddleware "github.com/oapi-codegen/gin-middleware"

	gen "github.com/kurmaevamir/avito-kitchen/internal/api/gen"
)

// openapiValidator строит middleware, которая валидирует каждый входящий
// запрос против OpenAPI-спеки (kin-openapi). Ловит required-поля,
// enum-нарушения, minimum/maximum и типы. Гарантирует, что спека ↔
// поведение не разъедутся: request, который смогли обработать хендлеры,
// уже прошёл контракт.
func openapiValidator() (gin.HandlerFunc, error) {
	swagger, err := gen.GetSpec()
	if err != nil {
		return nil, err
	}
	// URL сервера меняется между окружениями (compose, test, prod) —
	// зануляем, чтобы валидатор не отверг «неизвестный host».
	swagger.Servers = nil
	return ginmiddleware.OapiRequestValidatorWithOptions(swagger, &ginmiddleware.Options{
		Options: openapi3filter.Options{
			// Security-заголовок валидирует partnerAuth в отдельной middleware.
			AuthenticationFunc: openapi3filter.NoopAuthenticationFunc,
		},
		ErrorHandler: func(c *gin.Context, message string, code int) {
			c.AbortWithStatusJSON(code, gen.Error{
				Code:    "validation_failed",
				Message: message,
			})
		},
	}), nil
}
