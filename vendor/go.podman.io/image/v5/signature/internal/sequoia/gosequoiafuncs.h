/*
 * This file was automatically generated from sequoia.h,
 * which is covered by the following license:
 * SPDX-License-Identifier: Apache-2.0
 */
VOID_FUNC(void, sequoia_error_free, (struct SequoiaError *err_ptr), (err_ptr))
FUNC(struct SequoiaMechanism *, sequoia_mechanism_new_from_directory, (const char *dir_ptr, struct SequoiaError **err_ptr), (dir_ptr, err_ptr))
FUNC(struct SequoiaMechanism *, sequoia_mechanism_new_ephemeral, (struct SequoiaError **err_ptr), (err_ptr))
VOID_FUNC(void, sequoia_mechanism_free, (struct SequoiaMechanism *mechanism_ptr), (mechanism_ptr))
VOID_FUNC(void, sequoia_signature_free, (struct SequoiaSignature *signature_ptr), (signature_ptr))
FUNC(const uint8_t *, sequoia_signature_get_data, (const struct SequoiaSignature *signature_ptr, size_t *data_len), (signature_ptr, data_len))
VOID_FUNC(void, sequoia_verification_result_free, (struct SequoiaVerificationResult *result_ptr), (result_ptr))
FUNC(const uint8_t *, sequoia_verification_result_get_content, (const struct SequoiaVerificationResult *result_ptr, size_t *data_len), (result_ptr, data_len))
FUNC(const char *, sequoia_verification_result_get_signer, (const struct SequoiaVerificationResult *result_ptr), (result_ptr))
FUNC(struct SequoiaSignature *, sequoia_sign, (struct SequoiaMechanism *mechanism_ptr, const char *key_handle_ptr, const char *password_ptr, const uint8_t *data_ptr, size_t data_len, struct SequoiaError **err_ptr), (mechanism_ptr, key_handle_ptr, password_ptr, data_ptr, data_len, err_ptr))
FUNC(struct SequoiaVerificationResult *, sequoia_verify, (struct SequoiaMechanism *mechanism_ptr, const uint8_t *signature_ptr, size_t signature_len, struct SequoiaError **err_ptr), (mechanism_ptr, signature_ptr, signature_len, err_ptr))
VOID_FUNC(void, sequoia_import_result_free, (struct SequoiaImportResult *result_ptr), (result_ptr))
FUNC(size_t, sequoia_import_result_get_count, (const struct SequoiaImportResult *result_ptr), (result_ptr))
FUNC(const char *, sequoia_import_result_get_content, (const struct SequoiaImportResult *result_ptr, size_t index, struct SequoiaError **err_ptr), (result_ptr, index, err_ptr))
FUNC(struct SequoiaImportResult *, sequoia_import_keys, (struct SequoiaMechanism *mechanism_ptr, const uint8_t *blob_ptr, size_t blob_len, struct SequoiaError **err_ptr), (mechanism_ptr, blob_ptr, blob_len, err_ptr))
FUNC(int, sequoia_set_logger_consumer, (void (*consumer)(enum SequoiaLogLevel, const char *), struct SequoiaError **err_ptr), (consumer, err_ptr))
