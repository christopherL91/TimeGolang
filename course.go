package main

import (
	"gopkg.in/mgo.v2/bson"
)

type Course struct {
	ID   bson.ObjectId `json:"_id" bson:"_id"`
	Name string        `json:"name"`
	Code string        `json:"code"`
	Labs []Lab         `json:"labs"`
}

type Lab struct {
	Name string `json:"name"`
}
