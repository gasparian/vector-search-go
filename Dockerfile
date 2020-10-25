FROM golang:1.15-alpine
RUN mkdir /tmp/setup
WORKDIR /tmp/setup
RUN apk add build-base && \
    wget -q ftp://ftp.unidata.ucar.edu/pub/netcdf/netcdf-4/hdf5-1.8.13.tar.gz && \
    tar -xzf hdf5-1.8.13.tar.gz

WORKDIR /tmp/setup/hdf5-1.8.13
RUN ./configure  --prefix=/usr/local && \
    make && make install && \
    rm -rf /tmp/*

RUN mkdir -p "$GOPATH/src/vector-search-go/data"
WORKDIR $GOPATH/src/vector-search-go
COPY . .

RUN go mod tidy && go build -o /usr/bin/app ./main.go 
RUN go build -o /usr/bin/run_prep_data ./run_prep_data.go

EXPOSE 8080
ENTRYPOINT [ "sh" ]

# CMD [ "run_prep_data" ]


