package sql

import "strings"

// The built-in functions of each dialect, folded as the dialect folds names. A test calling only
// these calls no product code. The lists hold the functions of the dialect's documentation that a
// test is likely to write; a built-in missing here is counted as unresolved, never as a call.

func words2(fold func(string) string, lists ...string) map[string]bool {
	out := map[string]bool{}
	for _, l := range lists {
		for _, w := range strings.Fields(l) {
			out[fold(w)] = true
		}
	}
	return out
}

// standardFunctions are the functions most dialects have.
const standardFunctions = `abs avg ceil ceiling coalesce concat count cume_dist dense_rank exp first_value floor
greatest lag last_value lead least length ln log lower ltrim max min mod nth_value ntile nullif percent_rank power
rank replace round row_number rtrim sign sqrt substr substring sum trim upper cast extract position trunc
stddev variance stddev_pop stddev_samp var_pop var_samp corr covar_pop covar_samp lpad rpad reverse ascii chr
char_length character_length octet_length bit_length translate overlay left right repeat space instr`

var postgresqlFunctions = words2(strings.ToLower, standardFunctions, `
array_agg array_append array_cat array_dims array_fill array_length array_lower array_position array_positions
array_prepend array_remove array_replace array_to_json array_to_string array_upper cardinality unnest
string_agg string_to_array regexp_match regexp_matches regexp_replace regexp_split_to_array regexp_split_to_table
regexp_count regexp_instr regexp_like regexp_substr split_part strpos starts_with initcap btrim concat_ws format
quote_ident quote_literal quote_nullable md5 sha224 sha256 sha384 sha512 encode decode convert_from convert_to
to_char to_date to_number to_timestamp to_hex date_part date_trunc date_bin age make_date make_time make_timestamp
make_timestamptz make_interval justify_days justify_hours justify_interval isfinite now clock_timestamp
statement_timestamp transaction_timestamp timeofday current_setting set_config generate_series generate_subscripts
json_agg jsonb_agg json_object_agg jsonb_object_agg json_build_array jsonb_build_array json_build_object
jsonb_build_object json_array_elements jsonb_array_elements json_array_elements_text jsonb_array_elements_text
json_array_length jsonb_array_length json_each jsonb_each json_each_text jsonb_each_text json_extract_path
jsonb_extract_path json_extract_path_text jsonb_extract_path_text json_object_keys jsonb_object_keys json_populate_record
jsonb_populate_record json_populate_recordset jsonb_populate_recordset json_to_record jsonb_to_record
json_to_recordset jsonb_to_recordset json_typeof jsonb_typeof jsonb_set jsonb_insert jsonb_pretty jsonb_strip_nulls
jsonb_path_query jsonb_path_query_array jsonb_path_query_first jsonb_path_exists jsonb_path_match to_json to_jsonb
row_to_json json_strip_nulls bool_and bool_or every bit_and bit_or bit_xor xmlagg percentile_cont percentile_disc
mode regr_avgx regr_avgy regr_count regr_intercept regr_r2 regr_slope regr_sxx regr_sxy regr_syy random setseed
gen_random_uuid uuid_generate_v4 currval nextval setval lastval cbrt degrees radians pi sin cos tan asin acos atan
atan2 div gcd lcm factorial width_bucket scale num_nonnulls num_nulls has_table_privilege has_schema_privilege
has_function_privilege current_database current_schema current_schemas current_user session_user version
txid_current inet_client_addr host masklen netmask network family abbrev broadcast set_masklen text_to_array
array_ndims to_tsvector to_tsquery plainto_tsquery phraseto_tsquery websearch_to_tsquery ts_rank ts_rank_cd ts_headline
setweight strip tsvector_to_array lower_inc upper_inc lower_inf upper_inf isempty range_merge daterange tsrange
tstzrange int4range int8range numrange row format_type obj_description col_description to_regclass to_regproc
to_regtype exists any all some`)

var mysqlFunctions = words2(strings.ToLower, standardFunctions, `
adddate addtime aes_decrypt aes_encrypt any_value benchmark bin bit_count cast char charset coercibility collation
compress connection_id conv convert_tz crc32 curdate current_date current_time current_timestamp current_user curtime
database date date_add date_format date_sub datediff day dayname dayofmonth dayofweek dayofyear elt export_set
field find_in_set format found_rows from_base64 from_days from_unixtime get_format get_lock group_concat hex hour
if ifnull inet_aton inet_ntoa insert is_free_lock is_ipv4 is_ipv6 is_used_lock isnull json_array json_arrayagg
json_contains json_contains_path json_depth json_extract json_insert json_keys json_length json_merge_patch
json_merge_preserve json_object json_objectagg json_overlaps json_pretty json_quote json_remove json_replace
json_search json_set json_table json_type json_unquote json_valid json_value last_day last_insert_id lcase load_file
localtime localtimestamp locate log10 log2 make_set makedate maketime md5 microsecond minute month monthname now oct
ord period_add period_diff pi quarter quote radians rand regexp_instr regexp_like regexp_replace regexp_substr
release_lock row_count schema sec_to_time second session_user sha sha1 sha2 sleep soundex str_to_date strcmp subdate
substring_index subtime sysdate system_user time time_format time_to_sec timediff timestamp timestampadd timestampdiff
to_base64 to_days to_seconds truncate ucase uncompress unhex unix_timestamp user utc_date utc_time utc_timestamp uuid
uuid_short uuid_to_bin bin_to_uuid values version week weekday weekofyear year yearweek bit_and bit_or bit_xor
st_distance st_geomfromtext st_astext point`)

var sqliteFunctions = words2(strings.ToLower, standardFunctions, `
changes char glob hex ifnull iif instr last_insert_rowid like likelihood likely load_extension printf quote random
randomblob soundex sqlite_compileoption_get sqlite_compileoption_used sqlite_offset sqlite_source_id sqlite_version
total_changes typeof unicode unlikely zeroblob date time datetime julianday unixepoch strftime timediff group_concat
total json json_array json_array_length json_extract json_insert json_object json_patch json_remove json_replace
json_set json_type json_valid json_quote json_group_array json_group_object json_each json_tree format concat_ws
string_agg octet_length`)

var tsqlFunctions = words2(strings.ToLower, standardFunctions, `
approx_count_distinct checksum checksum_agg count_big grouping grouping_id string_agg cast convert try_cast
try_convert parse try_parse choose iif isnull isnumeric isdate datalength charindex patindex concat_ws format
quotename replicate soundex difference str stuff unicode nchar string_escape string_split translate trim
dateadd datediff datediff_big datefromparts datename datepart datetime2fromparts datetimefromparts
datetimeoffsetfromparts day eomonth getdate getutcdate isdate month smalldatetimefromparts switchoffset
sysdatetime sysdatetimeoffset sysutcdatetime timefromparts todatetimeoffset year at_time_zone newid newsequentialid
scope_identity ident_current identity object_id object_name object_schema_name schema_id schema_name db_id db_name
user_id user_name suser_name suser_sname host_name app_name error_message error_number error_severity error_state
error_line error_procedure xact_state rowcount_big json_value json_query json_modify isjson openjson openxml
openquery openrowset opendatasource compress decompress hashbytes binary_checksum col_length col_name columnproperty
objectproperty typeproperty serverproperty databaseproperty databasepropertyex session_context context_info
formatmessage trigger_nestlevel cursor_status is_member is_srvrolemember has_perms_by_name permissions
square log10 pi rand radians degrees atan2 atn2 cot`)

var oracleFunctions = words2(strings.ToUpper, standardFunctions, `
add_months last_day months_between new_time next_day to_char to_date to_number to_timestamp to_timestamp_tz
to_clob to_blob to_nchar to_dsinterval to_yminterval numtodsinterval numtoyminterval extract sessiontimezone
dbtimezone sys_extract_utc from_tz tz_offset nvl nvl2 decode coalesce lnnvl nanvl regexp_like regexp_replace
regexp_substr regexp_instr regexp_count listagg xmlagg xmlelement xmlforest xmlattributes json_value json_query
json_object json_array json_arrayagg json_objectagg json_table sys_guid uid user userenv sys_context ora_hash
standard_hash rawtohex hextoraw empty_blob empty_clob bitand cardinality collect powermultiset median
percentile_cont percentile_disc rowidtochar chartorowid soundex initcap concat nlssort nls_upper nls_lower
nls_initcap vsize dump greatest least width_bucket cosh sinh tanh atan2 remainder round trunc`)

var bigqueryFunctions = words2(func(s string) string { return s }, strings.ToUpper(standardFunctions), strings.ToLower(standardFunctions), `
ARRAY_AGG ARRAY_CONCAT ARRAY_LENGTH ARRAY_TO_STRING GENERATE_ARRAY GENERATE_DATE_ARRAY UNNEST STRUCT SAFE_CAST
SAFE_DIVIDE IF IFNULL COUNTIF LOGICAL_AND LOGICAL_OR STRING_AGG ANY_VALUE APPROX_COUNT_DISTINCT FORMAT FORMAT_DATE
FORMAT_TIMESTAMP PARSE_DATE PARSE_TIMESTAMP PARSE_DATETIME DATE DATETIME TIME TIMESTAMP DATE_ADD DATE_SUB DATE_DIFF
DATE_TRUNC TIMESTAMP_ADD TIMESTAMP_SUB TIMESTAMP_DIFF TIMESTAMP_TRUNC DATETIME_ADD DATETIME_DIFF DATETIME_TRUNC
CURRENT_DATE CURRENT_TIMESTAMP CURRENT_DATETIME CURRENT_TIME UNIX_SECONDS UNIX_MILLIS TIMESTAMP_SECONDS
TIMESTAMP_MILLIS JSON_EXTRACT JSON_EXTRACT_SCALAR JSON_QUERY JSON_VALUE TO_JSON_STRING SPLIT REGEXP_CONTAINS
REGEXP_EXTRACT REGEXP_EXTRACT_ALL REGEXP_REPLACE STARTS_WITH ENDS_WITH STRPOS FARM_FINGERPRINT MD5 SHA256
GENERATE_UUID ST_GEOGPOINT ST_DISTANCE`)

var snowflakeFunctions = words2(strings.ToUpper, standardFunctions, `
array_agg array_construct array_size array_contains array_to_string arrayagg object_construct object_keys
parse_json to_json to_variant to_array to_object flatten get get_path iff ifnull nvl nvl2 zeroifnull nullifzero
div0 listagg split split_part strtok regexp_like regexp_replace regexp_substr contains startswith endswith
dateadd datediff date_trunc date_part to_date to_timestamp to_timestamp_ntz to_timestamp_ltz to_timestamp_tz
to_char to_number to_decimal to_varchar to_boolean try_cast try_to_number try_to_date try_to_timestamp
current_date current_timestamp current_time sysdate getdate convert_timezone last_day dayname monthname
uuid_string hash md5 sha1 sha2 seq4 seq8 uniform random generator iff count_if approx_count_distinct hll
median mode percentile_cont percentile_disc result_scan`)
