#!/bin/sh
basepath=$(cd `dirname $0`; pwd)
echo "current path: ${basepath}"

cl=$(docker ps -a | grep basic_img-arm:1.0 | awk '{print $1}')
if [ ! ${cl} ]; then
   echo
else
    docker stop ${cl}
    docker rm ${cl}
fi

img=$(docker images | grep basic_img-arm | awk '{print $3}')
if [ ! ${img} ]; then
   echo
else
    docker rmi ${img}
fi

chmod a+x $basepath/appctl $basepath/appctl-daemon
docker build -t basic_img-arm:1.0 .
