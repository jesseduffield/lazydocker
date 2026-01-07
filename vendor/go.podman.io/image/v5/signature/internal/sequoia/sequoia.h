// SPDX-License-Identifier: Apache-2.0

#pragma once

#include <stdarg.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdlib.h>

typedef enum SequoiaErrorKind {
  SEQUOIA_ERROR_KIND_UNKNOWN,
  SEQUOIA_ERROR_KIND_INVALID_ARGUMENT,
  SEQUOIA_ERROR_KIND_IO_ERROR,
} SequoiaErrorKind;

typedef enum SequoiaLogLevel {
  SEQUOIA_LOG_LEVEL_UNKNOWN,
  SEQUOIA_LOG_LEVEL_ERROR,
  SEQUOIA_LOG_LEVEL_WARN,
  SEQUOIA_LOG_LEVEL_INFO,
  SEQUOIA_LOG_LEVEL_DEBUG,
  SEQUOIA_LOG_LEVEL_TRACE,
} SequoiaLogLevel;

typedef struct SequoiaImportResult SequoiaImportResult;

typedef struct SequoiaMechanism SequoiaMechanism;

typedef struct SequoiaSignature SequoiaSignature;

typedef struct SequoiaVerificationResult SequoiaVerificationResult;

typedef struct SequoiaError {
  enum SequoiaErrorKind kind;
  char *message;
} SequoiaError;

void sequoia_error_free(struct SequoiaError *err_ptr);

struct SequoiaMechanism *sequoia_mechanism_new_from_directory(const char *dir_ptr,
                                                              struct SequoiaError **err_ptr);

struct SequoiaMechanism *sequoia_mechanism_new_ephemeral(struct SequoiaError **err_ptr);

void sequoia_mechanism_free(struct SequoiaMechanism *mechanism_ptr);

void sequoia_signature_free(struct SequoiaSignature *signature_ptr);

const uint8_t *sequoia_signature_get_data(const struct SequoiaSignature *signature_ptr,
                                          size_t *data_len);

void sequoia_verification_result_free(struct SequoiaVerificationResult *result_ptr);

const uint8_t *sequoia_verification_result_get_content(const struct SequoiaVerificationResult *result_ptr,
                                                       size_t *data_len);

const char *sequoia_verification_result_get_signer(const struct SequoiaVerificationResult *result_ptr);

struct SequoiaSignature *sequoia_sign(struct SequoiaMechanism *mechanism_ptr,
                                      const char *key_handle_ptr,
                                      const char *password_ptr,
                                      const uint8_t *data_ptr,
                                      size_t data_len,
                                      struct SequoiaError **err_ptr);

struct SequoiaVerificationResult *sequoia_verify(struct SequoiaMechanism *mechanism_ptr,
                                                 const uint8_t *signature_ptr,
                                                 size_t signature_len,
                                                 struct SequoiaError **err_ptr);

void sequoia_import_result_free(struct SequoiaImportResult *result_ptr);

size_t sequoia_import_result_get_count(const struct SequoiaImportResult *result_ptr);

const char *sequoia_import_result_get_content(const struct SequoiaImportResult *result_ptr,
                                              size_t index,
                                              struct SequoiaError **err_ptr);

struct SequoiaImportResult *sequoia_import_keys(struct SequoiaMechanism *mechanism_ptr,
                                                const uint8_t *blob_ptr,
                                                size_t blob_len,
                                                struct SequoiaError **err_ptr);

int sequoia_set_logger_consumer(void (*consumer)(enum SequoiaLogLevel level, const char *message),
                                struct SequoiaError **err_ptr);
