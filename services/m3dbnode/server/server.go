	"errors"
	m3clusterkv "github.com/m3db/m3cluster/kv"
	m3clusterkvmem "github.com/m3db/m3cluster/kv/mem"
	"github.com/m3db/m3cluster/shard"
var (
	errNilRetention = errors.New("namespace retention options cannot be empty")
)

		logger.Fatalf("could not parse new file mode: %v", err)
		logger.Fatalf("could not parse new directory mode: %v", err)
		SetMmapEnableHugeTLB(mmap.HugeTLB.Enabled).
		SetMmapHugeTLBThreshold(mmap.HugeTLB.Threshold).
		logger.Fatalf("unknown commit log queue size type: %v",
	// Set the series cache policy
	seriesCachePolicy := cfg.Cache.SeriesConfiguration().Policy
	opts = opts.SetSeriesCachePolicy(seriesCachePolicy)

	switch seriesCachePolicy {
	case series.CacheAll:
		// No options needed to be set
	default:
		// All other caching strategies require retrieving series from disk
		// to service a cache miss
		retrieverOpts := fs.NewBlockRetrieverOptions().
			SetBytesPool(opts.BytesPool()).
			SetSegmentReaderPool(opts.SegmentReaderPool())
		if blockRetrieveCfg := cfg.BlockRetrieve; blockRetrieveCfg != nil {
			retrieverOpts = retrieverOpts.
				SetFetchConcurrency(blockRetrieveCfg.FetchConcurrency)
		}
		blockRetrieverMgr := block.NewDatabaseBlockRetrieverManager(
			func(md namespace.Metadata) (block.DatabaseBlockRetriever, error) {
				retriever := fs.NewBlockRetriever(retrieverOpts, fsopts)
				if err := retriever.Open(md); err != nil {
					return nil, err
				}
				return retriever, nil
			})
		opts = opts.SetDatabaseBlockRetrieverManager(blockRetrieverMgr)
	}

	var (
		topoInit topology.Initializer
		kv       m3clusterkv.Store
	)
	switch {
	case cfg.ConfigService != nil:
		logger.Info("creating dynamic config service client with m3cluster")
		configSvcClientOpts := cfg.ConfigService.NewOptions().
			SetInstrumentOptions(
				instrument.NewOptions().
					SetLogger(logger).
					SetMetricsScope(scope))
		configSvcClient, err := etcdclient.NewConfigServiceClient(configSvcClientOpts)
		if err != nil {
			logger.Fatalf("could not create m3cluster client: %v", err)
		}
		dynamicOpts := namespace.NewDynamicOptions().
			SetInstrumentOptions(iopts).
			SetConfigServiceClient(configSvcClient).
			SetNamespaceRegistryKey(kvconfig.NamespacesKey)
		nsInit := namespace.NewDynamicInitializer(dynamicOpts)
		opts = opts.SetNamespaceInitializer(nsInit)
		serviceID := services.NewServiceID().
			SetName(cfg.ConfigService.Service).
			SetEnvironment(cfg.ConfigService.Env).
			SetZone(cfg.ConfigService.Zone)
		topoOpts := topology.NewDynamicOptions().
			SetConfigServiceClient(configSvcClient).
			SetServiceID(serviceID).
			SetQueryOptions(services.NewQueryOptions().SetIncludeUnhealthy(true)).
			SetInstrumentOptions(opts.InstrumentOptions()).
			SetHashGen(sharding.NewHashGenWithSeed(cfg.Hashing.Seed))

		topoInit = topology.NewDynamicInitializer(topoOpts)

		kv, err = configSvcClient.KV()
		if err != nil {
			logger.Fatalf("could not create KV client, %v", err)
		}

	case cfg.StaticConfig != nil && cfg.StaticConfig.TopologyConfig != nil && cfg.StaticConfig.Namespaces != nil:
		logger.Info("creating static config service client with m3cluster")

		shardSet, hostShardSets, err := newStaticShardSet(cfg.StaticConfig.TopologyConfig.Shards, cfg.ListenAddress)
		if err != nil {
			logger.Fatalf("unable to create shard set for static config: %v", err)
		}
		staticOptions := topology.NewStaticOptions().
			SetReplicas(1).
			SetHostShardSets(hostShardSets).
			SetShardSet(shardSet)

		nsList := []namespace.Metadata{}
		for _, ns := range cfg.StaticConfig.Namespaces {
			md, err := newNamespaceMetadata(ns)
			if err != nil {
				logger.Fatalf("unable to create metadata for static config: %v", err)
			}
			nsList = append(nsList, md)
		}
		nsInitStatic := namespace.NewStaticInitializer(nsList)
		topoInit = topology.NewStaticInitializer(staticOptions)
		opts = opts.SetNamespaceInitializer(nsInitStatic)

		kv = m3clusterkvmem.NewStore()

	default:
		logger.Fatal("config service or static configuration required")
	}
	bs, err := cfg.Bootstrap.New(opts, m3dbClient)
			updated, err := cfg.Bootstrap.New(opts, m3dbClient)
		logger.Infof("bytes pool registering bucket capacity=%d, size=%d, "+
			"refillLowWatermark=%f, refillHighWatermark=%f",
	logger.Infof("bytes pool %s init", policy.Type)

func newStaticShardSet(numShards int, listenAddress string) (sharding.ShardSet, []topology.HostShardSet, error) {
	var (
		shardSet      sharding.ShardSet
		hostShardSets []topology.HostShardSet
		shardIDs      []uint32
		err           error
	)

	for i := uint32(0); i < uint32(numShards); i++ {
		shardIDs = append(shardIDs, i)
	}

	shards := sharding.NewShards(shardIDs, shard.Available)
	shardSet, err = sharding.NewShardSet(shards, sharding.DefaultHashFn(1))
	if err != nil {
		return nil, nil, err
	}

	host := topology.NewHost("localhost", listenAddress)
	hostShardSet := topology.NewHostShardSet(host, shardSet)
	hostShardSets = append(hostShardSets, hostShardSet)

	return shardSet, hostShardSets, nil
}

func newNamespaceMetadata(cfg config.StaticNamespaceConfiguration) (namespace.Metadata, error) {
	if cfg.Retention == nil {
		return nil, errNilRetention
	}
	if cfg.Options == nil {
		cfg.Options = &config.StaticNamespaceOptions{
			NeedsBootstrap:      true,
			NeedsFilesetCleanup: true,
			NeedsFlush:          true,
			NeedsRepair:         true,
			WritesToCommitLog:   true,
		}
	}
	md, err := namespace.NewMetadata(
		ts.StringID(cfg.Name),
		namespace.NewOptions().
			SetNeedsBootstrap(cfg.Options.NeedsBootstrap).
			SetNeedsFilesetCleanup(cfg.Options.NeedsFilesetCleanup).
			SetNeedsFlush(cfg.Options.NeedsFlush).
			SetNeedsRepair(cfg.Options.NeedsRepair).
			SetWritesToCommitLog(cfg.Options.WritesToCommitLog).
			SetRetentionOptions(
				retention.NewOptions().
					SetBlockSize(cfg.Retention.BlockSize).
					SetRetentionPeriod(cfg.Retention.RetentionPeriod).
					SetBufferFuture(cfg.Retention.BufferFuture).
					SetBufferPast(cfg.Retention.BufferPast).
					SetBlockDataExpiry(cfg.Retention.BlockDataExpiry).
					SetBlockDataExpiryAfterNotAccessedPeriod(cfg.Retention.BlockDataExpiryAfterNotAccessPeriod)))
	if err != nil {
		return nil, err
	}

	return md, nil
}