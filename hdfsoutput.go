package hdfs

import (
        "bytes"
        "errors"
        "fmt"
        "os"
        "regexp"
        "strconv"
        "strings"
        "sync/atomic"
        "time"

        "github.com/mozilla-services/heka/message"

        "bitbucket.org/tebeka/strftime"
        "github.com/Sirupsen/logrus"
        webhdfs "github.com/vladimirvivien/gowfs"
        "github.com/mozilla-services/heka/pipeline"
)

var varMatcher *regexp.Regexp

type HDFSOutput struct {
        *HDFSOutputConfig
        fs *webhdfs.FileSystem

        processMessageCount    int64
        processMessageFailures int64
}

func (hdfs *HDFSOutput) ConfigStruct() interface{} {
        return &HDFSOutputConfig{
                Host:        "localhost:14000",
                Timeout:     15,
                KeepAlive:   false,
                Perm:        0644,
                Overwrite:   false,
                Blocksize:   134217728,
                Replication: 3,
                Buffersize:  4096,
                Timestamp:   false,
                Interpolate: false,
        }
}

// ConfigStruct for HDFSOutput plugin.
type HDFSOutputConfig struct {
        // WebHDFS or HTTPfs host and port (default localhost:14000)
        Host string `toml:"host"`

        // User to create connection with
        User string

        // Connection timeout in seconds to HDFS (default 15)
        Timeout uint `toml:"timeout"`

        // DisableKeepAlives (default false).
        KeepAlive bool `toml:"keepalive"`

        // Full output file path.
        Path string

        // Append epoch in milliseconds.  E.g. /<path>/<on>/<hdfs>/syslog.1407245278657
        Timestamp bool

        // Extension to append to "Path".  This can be used to denote filetype.
        Extension string

        // Interpolate Path from Fields. (default false).
        // E.g. "/tmp/${server}.txt" -> "/tmp/web01.txt" where Field[server] = "web01"
        Interpolate bool

        // Output file permissions (default "0700").
        Perm os.FileMode `toml:"perm"`

        // Overwrite HDFS file if exists (default false).
        Overwrite bool `toml:"overwrite"`

        // Blocksize (default 134217728, (128MB)).
        Blocksize uint64 `toml:"blocksize"`

        // Replication (default 3)
        Replication uint16 `toml:"replication"`

        // Size of the buffer used in transferring data (default 4096).
        Buffersize uint `toml:"buffersize"`

        // Specifies whether or not Heka's stream framing will be applied to the
        // output. We do some magic to default to true if ProtobufEncoder is used,
        // false otherwise.
        UseFraming *bool `toml:"use_framing"`
}

func (hdfs *HDFSOutput) Init(config interface{}) (err error) {
        conf := config.(*HDFSOutputConfig)
        hdfs.HDFSOutputConfig = conf

        // Allow setting of 0 to indicate default
        if conf.Blocksize < 0 {
                err = fmt.Errorf("Parameter 'blocksize' needs to be greater than 0.")
                return
        }
        if conf.Timeout < 0 {
                err = fmt.Errorf("Parameter 'timeout' needs to be greater than 0.")
                return
        }
        if conf.Replication < 0 {
                err = fmt.Errorf("Parameter 'replication' needs to be greater than 0.")
                return
        }
        if conf.Buffersize < 0 {
                err = fmt.Errorf("Parameter 'buffersize' needs to be greater than 0.")
                return
        }
        logrus.Infof("hdfsconfig:%#v", *conf)

        return
}

// Creates connection to HDFS.
func (hdfs *HDFSOutput) hdfsConnection() (err error) {
        conf := webhdfs.NewConfiguration()
        conf.Addr = hdfs.Host
        conf.User = hdfs.User
        conf.ConnectionTimeout = time.Second * time.Duration(hdfs.Timeout)
        conf.DisableKeepAlives = hdfs.KeepAlive
        hdfs.fs, err = webhdfs.NewFileSystem(*conf)
        logrus.Infof("hdfsConnection::hdfsconfig:%#v", *conf)

        return
}

// Writes to HDFS using go-webhdfs.Create
func (hdfs *HDFSOutput) hdfsWrite(data []byte, fields map[string]string) (err error) {
        if err = hdfs.hdfsConnection(); err != nil {
                return fmt.Errorf("HDFSOutput unable to reopen HDFS Connection: %s", err)
        }

        path, err := strftime.Format(hdfs.Path, time.Now())
        if err != nil {
                return
        }

        if hdfs.Interpolate == true {
                matched := varMatcher.FindAllStringSubmatch(hdfs.Path, -1)
                for _, entry := range matched {
                        path = strings.Replace(path, entry[0], fields[entry[1]], -1)
                }
        }

        if hdfs.Timestamp == true {
                now := time.Now().UnixNano()
                path = path + "." + strconv.FormatInt(now/1e6, 10)
        }

        if hdfs.Extension != "" {
                path = path + "." + hdfs.Extension
        }

        statu, err := hdfs.fs.GetFileStatus(webhdfs.Path{Name: path})
        if err != nil {
                logrus.Errorf("GetFileStatus err:%v", err)
        }
        logrus.Debugf("status:%s", statu.PathSuffix)

        ok, err := hdfs.fs.Append(bytes.NewReader(data), webhdfs.Path{Name: path}, int(hdfs.Buffersize))
        if ok {
                return nil
        }

        _, err = hdfs.fs.Create(
                bytes.NewReader(data),
                webhdfs.Path{Name: path},
                hdfs.Overwrite,
                hdfs.Blocksize,
                hdfs.Replication,
                hdfs.Perm,
                hdfs.Buffersize,
        )

        return
}

func (hdfs *HDFSOutput) Run(or pipeline.OutputRunner, h pipeline.PluginHelper) (err error) {
        if or.Encoder() == nil {
                return errors.New("Encoder must be specified.")
        }

        var (
                e        error
                outBytes []byte
        )
        fieldMap := make(map[string]string)
        inChan := or.InChan()

        for pack := range inChan {
                atomic.AddInt64(&hdfs.processMessageCount, 1)
                outBytes, e = or.Encode(pack)
                for _, field := range pack.Message.Fields {
                        fieldMap[field.GetName()] = field.ValueString[0]
                }
                pack.Recycle(nil)
                if e != nil {
                        atomic.AddInt64(&hdfs.processMessageFailures, 1)
                        or.LogError(e)
                        continue
                }
                if e = hdfs.hdfsWrite(outBytes, fieldMap); e != nil {
                        atomic.AddInt64(&hdfs.processMessageFailures, 1)
                        or.LogError(e)
                }
        }

        return
}
func (hdfs *HDFSOutput) ReportMsg(msg *message.Message) error {

        message.NewInt64Field(msg, "ProcessMessageCount",
                atomic.LoadInt64(&hdfs.processMessageCount), "count")
        message.NewInt64Field(msg, "ProcessMessageFailures",
                atomic.LoadInt64(&hdfs.processMessageFailures), "count")

        return nil
}

func init() {
        varMatcher, _ = regexp.Compile("\\${(\\w+)}")
        pipeline.RegisterPlugin("HDFSOutput", func() interface{} {
                return new(HDFSOutput)
        })
}
