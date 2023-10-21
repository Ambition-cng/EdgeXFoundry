#! /bin/bash
docker stop $(docker ps -aq)
docker rm $(docker ps -aq)
yes | docker volume prune
docker volume rm edgex-compose_consul-config edgex-compose_consul-data edgex-compose_db-data edgex-compose_kuiper-data
