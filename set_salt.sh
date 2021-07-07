#!/bin/bash

sed -i -- 's/SALT=/SALT='$(head /dev/urandom | tr -dc A-Za-z0-9 | head -c32 ; echo '')'/g' .env