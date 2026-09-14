/**
 * Shared type definitions for the extension (consumed via JSDoc imports).
 *
 * @typedef {Object} Settings
 * @property {string} baseUrl Platform site origin, e.g. `http://localhost:8080` (no trailing slash, no `/api/v1`).
 * @property {string} token   Personal access token with the `br_` prefix.
 *
 * @typedef {Object} ProjectSummary
 * @property {number} id
 * @property {string} name
 * @property {string} slug
 *
 * @typedef {Object} BugSummary
 * @property {number} id
 * @property {string} title
 * @property {string} status
 *
 * @typedef {Object} AttachmentSummary
 * @property {number} id
 * @property {string} filename
 * @property {string} content_type
 */

export {};
