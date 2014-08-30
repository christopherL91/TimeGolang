package main

import (
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
