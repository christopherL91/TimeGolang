package main

import (
	"gopkg.in/mgo.v2/bson"
)

type User struct {
	ID     bson.ObjectId `json:"_id" bson:"_id"`
	Name   string        `json:"name"`
	KTH_ID string        `json:"kthid"`
}
