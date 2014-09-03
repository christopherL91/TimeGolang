package main

import (
	"gopkg.in/mgo.v2/bson"
)

type User struct {
	ID    bson.ObjectId `json:"_id,omitempty" bson:"_id,omitempty"`
	Name  string        `json:"name"`
	KTHID string        `json:"kthid"`
}
