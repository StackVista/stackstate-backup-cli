export AWS_ACCESS_KEY_ID
AWS_ACCESS_KEY_ID="$(cat /aws-keys/accesskey)"
export AWS_SECRET_ACCESS_KEY
AWS_SECRET_ACCESS_KEY="$(cat /aws-keys/secretkey)"

export BACKUP_V2_LOCATION="${BACKUP_STACKGRAPH_BUCKET_NAME}/${BACKUP_STACKGRAPH_S3_PREFIX}v2/"

TYPESAFE_ESCAPED_BUCKET=$(echo "${BACKUP_STACKGRAPH_BUCKET_NAME}" | sed 's/_/___/g; s/-/__/g; s/\./_/g')
# HACK: We configure hbase here through typesafe config. However, typesafe does not support a key being both object and
# string (as in `endpoint = "string"` and `endpoint.region = "us-east-1"`
# We build a little hack there, which allows postfixing endpoint as endpoint__, which gets stripped when transforming typesafe to hbase conf
AWS_BUCKET_ENDPOINT_VAR="CONFIG_FORCE_fs_s3a_bucket_${TYPESAFE_ESCAPED_BUCKET}_endpoint___"
export "${AWS_BUCKET_ENDPOINT_VAR}=http://${S3_ENDPOINT}"
AWS_BUCKET_REGION_VAR="CONFIG_FORCE_fs_s3a_bucket_${TYPESAFE_ESCAPED_BUCKET}_endpoint_region"
export "${AWS_BUCKET_REGION_VAR}=us-east-1"
AWS_BUCKET_ACCESS_KEY_VAR="CONFIG_FORCE_fs_s3a_bucket_${TYPESAFE_ESCAPED_BUCKET}_access_key"
export "${AWS_BUCKET_ACCESS_KEY_VAR}=$(cat /aws-keys/accesskey)"
AWS_BUCKET_SECRET_KEY_VAR="CONFIG_FORCE_fs_s3a_bucket_${TYPESAFE_ESCAPED_BUCKET}_secret_key"
export "${AWS_BUCKET_SECRET_KEY_VAR}=$(cat /aws-keys/secretkey)"

AWS_BUCKET_PATH_STYLE_VAR="CONFIG_FORCE_fs_s3a_bucket_${TYPESAFE_ESCAPED_BUCKET}_path_style_access"
export "${AWS_BUCKET_PATH_STYLE_VAR}=true"
AWS_BUCKET_CONNECTION_SSL_ENABLED_VAR="CONFIG_FORCE_fs_s3a_bucket_${TYPESAFE_ESCAPED_BUCKET}_connection_ssl_enabled"
export "${AWS_BUCKET_CONNECTION_SSL_ENABLED_VAR}=false"
AWS_BUCKET_AWS_CREDENTIALS_PROVIDER_VAR="CONFIG_FORCE_fs_s3a_bucket_${TYPESAFE_ESCAPED_BUCKET}_aws_credentials_provider"
export "${AWS_BUCKET_AWS_CREDENTIALS_PROVIDER_VAR}=org.apache.hadoop.fs.s3a.SimpleAWSCredentialsProvider"

# Increase the hbase client timeout for bulk operations.
export CONFIG_FORCE_hbase_rpc_timeout="120000"

