package main

import (
	mgo "gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
)

func updateLoginInfo(session *mgo.Session, data map[string]interface{}) string {
	c := session.DB("parkour").C("users")
	session.SetMode(mgo.Monotonic, true)

	kthid := data["kthid"].(string)

	user := make(map[string]interface{})
	err := c.Find(bson.M{"kthid": kthid}).One(&user)
	if err != nil {
		id := bson.NewObjectId()
		data["_id"] = id
		//User not found create a new one
		c.Insert(bson.M(data))
		return id.Hex()
	}
	id := user["_id"].(bson.ObjectId)
	c.UpdateId(id, bson.M(data))
	return id.Hex()
}
