# heka_filter_decoder
mozala heka  decoder

> filer pack by filter_formula, used with multidecoder

```
[myfilter_decoder]
type="FilterDecoder"
route_type="mylogtype"
filter_formula = "Fields[created] =~ /%TIMESTAMP%/ || Fields[foo] == ‘alternate’"  
```

 [here it is](http://hekad.readthedocs.io/en/v0.10.0/message_matcher.html) filter_formula format

# heka_filter_decoder
mozala heka  decoder

> filer pack by filter_formula, used with multidecoder

```
[kafkainput]
type="CompatibleKafkaInput"
brokers="ip1:port1,ip2:port2"
topics = ["top1", "top2"]
group = "consumergroup"
```
