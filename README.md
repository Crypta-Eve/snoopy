
# Snoopy

## About the project

Snoopy is a very lightweight and simplistic, anonomised usage tracker

## Getting started

The recomended way to run this service is through using the provided Dockerfile. However feel free to run it from source.

This project serves my needs and as such support will be limited, though feel free to reach out with issues or pull requests.

## Usage

The project will connect to a mysql compatible database as defined by the following environment variables:
 - MARIA_HOST
 - MARIA_USER
 - MARIA_PASS
 - MARIA_DATABASE

(note, this does not have to be mariadb)

Also expected is an environment variable `SALT` which is used to salt IP addresses prior to hashing.

Lastly, you can adjust the session time (default 10 minutes) using the `SESS_TIME` env var.
The session time is how long of a gap is required between hits to the same service

### Endpoints

The following endpoints are configured for the service

#### /ping
Accessible as a system health endpoint, returns a period (`.`).

#### /snoopy/{slug}
This is the tracking enpoint, the DB record consists of a salted and hashed IP, and the `slug` identifier.
