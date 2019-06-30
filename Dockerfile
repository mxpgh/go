FROM mxpan/alpine-arm:1.0
RUN mkdir /usr/local/monitor
COPY ./appctl-daemon ./monitor.cfg /usr/local/monitor/
COPY ./appctl /usr/bin/
ENTRYPOINT [ "/usr/local/monitor/appctl-daemon" ]



