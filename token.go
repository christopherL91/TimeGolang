package main

import (
	"github.com/christopherL91/TimeGolang/gin"
	jwt_lib "github.com/dgrijalva/jwt-go"
)

// Generate a valid token. Put this is in the auth header when making calls to auth routes.
func generateToken(secret []byte, claims *map[string]interface{}) (string, error) {
	token := jwt_lib.New(jwt_lib.GetSigningMethod("HS256"))
	token.Claims = *claims
	tokenString, err := token.SignedString(secret)
	if err != nil {
		return "", err
	}
	return tokenString, nil
}

// Gin middleware. Checks for valid tokens in the http auth header.
func tokenMiddleWare(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := jwt_lib.ParseFromRequest(c.Request, func(t *jwt_lib.Token) (interface{}, error) {
			return []byte(secret), nil
		})
		if err != nil {
			c.Fail(401, err)
			return
		}
		c.Set("user", token.Claims)
	}
}
