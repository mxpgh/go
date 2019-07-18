#!/bin/sh
if [ -z "$1" ]
then
    echo 'Error: 缺少参数：升级镜像文件绝对路径.'
    exit 1
fi

echo "开始更新镜像..."
updimg=$1
echo "镜像路径: ${updimg}"

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

docker load < ${updimg}
docker run -idt --name app-ctl-test basic_img-arm:1.0
imgv=$(docker exec app-ctl-test appctl -version container)
docker stop app-ctl-test
docker rm app-ctl-test

echo ""
echo "镜像更新成功，当前版本: ${imgv}"
echo "更新完成."





