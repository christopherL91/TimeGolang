package main

import (
	"gopkg.in/mgo.v2/bson"
)

type Course struct {
	ID   bson.ObjectId `json:"_id,omitempty" bson:"_id,omitempty"`
	Name string        `json:"name" binding:"required"`
	Code string        `json:"code" binding:"required"`
	Labs []Lab         `json:"labs" binding:"required"`
}

type Lab struct {
	ID   bson.ObjectId `json:"_id" bson:"_id"`
	Name string        `json:"name" binding:"required"`
}
