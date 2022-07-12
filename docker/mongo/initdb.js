#!/bin/env mongo

// note: mongo creates the database when getting it
db = db.getSiblingDB('admin-server-db')
db.createCollection('logs')



