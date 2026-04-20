/**
 * Error raised when localization fails
 *
 * @export
 * @class LocaleDataError
 * @extends {Error}
 */
export class LocaleDataError extends Error {
    /**
     * Creates an instance of LocaleDataError
     *
     * @param {string} message the error message
     * @param {Error} [innerError] the optional inner error
     * @memberof LocaleDataError
     */
    constructor(message: string, public innerError?: Error) {
        super(message);
        this.name = LocaleDataError.name;
    }
}
