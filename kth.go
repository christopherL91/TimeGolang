package main

type KTH_User struct {
	ID string `xml:"cas:serviceResponse>cas:authenticationSuccess>cas:user"`
}
