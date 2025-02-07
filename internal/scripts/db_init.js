// ibc_chain表
db.ibc_chain.createIndex({'chain_id': -1}, {background: true, unique: true});

// chain_registry
db.chain_registry.createIndex({
    "chain_id": 1
}, {
    unique: true,
    background: true
});

// ibc_relayer表
db.ibc_relayer.createIndex({
    "chain_a": -1,
    "channel_a": -1,
    "chain_a_address": -1
}, {background: true, unique: true});

db.ibc_relayer.createIndex({
    "chain_b": -1,
    "channel_b": -1,
    "chain_b_address": -1
}, {background: true, unique: true});

// ibc_relayer_config表

db.ibc_relayer_config.createIndex({
    "relayer_pair_id": 1
}, {background: true, unique: true});

// ibc_relayer_statistics表

db.ibc_relayer_statistics.createIndex({
    "transfer_base_denom": 1,
    "address": 1,
    "statistic_id": 1,
    "segment_start_time": -1,
    "segment_end_time": -1
}, {
    name: "relayer_statistics_unique",
    unique: true,
    background: true
});


// ibc_channel表

db.ibc_channel.createIndex({
    "channel_id": 1
}, {background: true, unique: true});


// ibc_channel_statistics表

db.ibc_channel_statistics.createIndex({
    "channel_id": 1,
    "base_denom": 1,
    "base_denom_chain_id": 1,
    "segment_start_time": -1,
    "segment_end_time": -1
}, {
    name: "channel_statistics_unique",
    unique: true,
    background: true
});

// ibc_token表

db.ibc_token.createIndex({
    "base_denom": 1,
    "chain_id": 1
}, {background: true, unique: true});

// ibc_token_statistics表

db.ibc_token_statistics.createIndex({
    "base_denom": 1,
    "base_denom_chain_id": 1,
    "segment_start_time": -1,
    "segment_end_time": -1
}, {
    unique: true,
    background: true
});

// ibc_token_trace表
db.ibc_token_trace.createIndex({
    "denom": 1,
    "chain_id": 1,
}, {
    background: true,
    unique: true
});

// ibc_token_trace_statistics表
db.ibc_token_trace_statistics.createIndex({
    "denom": 1,
    "chain_id": 1,
    "segment_start_time": -1,
    "segment_end_time": -1
}, {
    unique: true,
    background: true
});


// ex_ibc_tx表
db.ex_ibc_tx.createIndex({
    "sc_tx_info.hash": -1,
}, {
    background: true
});
db.ex_ibc_tx.createIndex({
    "dc_tx_info.hash": -1,
}, {
    background: true
});

db.ex_ibc_tx.createIndex({
    "refunded_tx_info.hash": -1,
}, {
    background: true
});

db.ex_ibc_tx.createIndex({
    "status": 1,
    "sc_tx_info.status": 1
}, {
    background: true
});

db.ex_ibc_tx.createIndex({
    "base_denom": 1,
    "status": 1
}, {
    background: true
});

db.ex_ibc_tx.createIndex({
    "dc_tx_info.status": 1
}, {
    background: true
});

db.ex_ibc_tx.createIndex({
    "status": -1,
    "tx_time": -1
}, {
    background: true
});

db.ex_ibc_tx.createIndex({
    "create_at": 1,
    "status": 1
}, {
    background: true
});


// ex_ibc_tx_latest表

db.ex_ibc_tx_latest.createIndex({
    "denoms.sc_denom" : -1,
}, {
    background: true
});

db.ex_ibc_tx_latest.createIndex({
    "denoms.dc_denom" : -1,
}, {
    background: true
});

db.ex_ibc_tx_latest.createIndex({
    "sc_tx_info.hash": -1,
}, {
    background: true
});

db.ex_ibc_tx_latest.createIndex({
    "dc_tx_info.hash": -1,
}, {
    background: true
});

db.ex_ibc_tx_latest.createIndex({
    "refunded_tx_info.hash": -1,
}, {
    background: true
});

db.ex_ibc_tx_latest.createIndex({
    "status": -1,
    "tx_time": -1
}, {
    background: true
});

db.ex_ibc_tx_latest.createIndex({
    "base_denom": 1,
    "status": 1
}, {
    background: true
});
db.ex_ibc_tx_latest.createIndex({
    "dc_tx_info.status": 1
}, {
    background: true
});
db.ex_ibc_tx_latest.createIndex({
    "status": 1,
    "sc_tx_info.status": 1
}, {
    background: true
});
db.ex_ibc_tx_latest.createIndex({
    "sc_chain_id": 1,
    "status": 1
}, {
    background: true
});
db.ex_ibc_tx_latest.createIndex({
    "dc_chain_id": 1,
    "status": 1
}, {
    background: true
});
db.ex_ibc_tx_latest.createIndex({
    "sc_chain_id": 1,
    "dc_chain_id": 1,
    "status": 1
}, {
    background: true
});

db.ex_ibc_tx_latest.createIndex({
    "create_at": 1,
    "status": 1
}, {
    background: true
});

// sync_{chain_id}_tx表
db.sync_xxxx_tx.createIndex({"tx_hash": -1,"height": -1},{unique: true, background: true});
db.sync_xxxx_tx.createIndex({"height": -1},{background: true});
db.sync_xxxx_tx.createIndex({"types": -1,"height": -1},{background: true});
db.sync_xxxx_tx.createIndex({"msgs.msg.packet_id":-1},{background: true});
db.sync_xxxx_tx.createIndex({"msgs.msg.signer": 1,"msgs.type": 1,"time": 1},{background: true});

// ex_search_record
db.ex_search_record.createIndex({
    "create_at": 1
}, {
    expireAfterSeconds: 31536000
})
