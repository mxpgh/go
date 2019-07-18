#!/bin/sh
basepath=$(cd `dirname $0`; pwd)
echo "current path: ${basepath}"

img1=$(docker images | grep basic_img-arm| awk '{if($2=="1.0"){print $3}}')
if [ -n "${img1}" ]; then
    c1=$(docker ps -a | grep ${img1} | awk '{print $1}')
    if [ -n "${cl}" ]; then
        docker stop ${cl}
        docker rm ${cl}
    fi
fi

c2=$(docker ps -a | grep basic_img-arm:1.0 | awk '{print $1}')
if [ -n "${c2}" ]]; then
    docker stop ${c2}
    docker rm ${c2}
fi

img2=$(docker images | grep basic_img-arm | awk '{print $3}')
if [ -n "${img2}" ]; then
    docker rmi ${img2}
fi

chmod a+x $basepath/appctl $basepath/appctl-daemon
docker build -t basic_img-arm:1.0 .
