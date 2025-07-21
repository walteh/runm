
FROM alpine:3.22 AS primary

RUN mkdir -p /primarytest/testdir

FROM primary AS export

COPY --from=data / /appdata

RUN ls -lash /appdata

RUN --mount=type=cache,target=/appdatacache <<EOF
echo "-------- listing /primarytest --------"
ls -lashr /primarytest
echo "-------- listing /appdata --------"
ls -lashr /appdata
echo "-------- listing /appdatacache --------"
ls -lashr /appdatacache
echo "-------- wgetting time.json --------"
wget -q -O /primarytest/testdir/time.json https://timeapi.io/api/Time/current/zone?timeZone=UTC
echo "-------- copying data to cache --------"
cp -r /appdata/* /appdatacache/
echo "-------- listing /primarytest/testdir --------"
ls -lashr /primarytest
echo "-------- listing /primarytest/testdir/time.json --------"
head -n 1 /primarytest/testdir/time.json
echo "-------- listing /appdata --------"
ls -lashr /appdata
echo "-------- listing /appdatacache --------"
ls -lashr /appdatacache
echo "-------- done --------"
EOF



