import { LocaleData } from '../locale-data/locale-data.model';

/**
 * Error raised when command parameter are invalid
 *
 * @export
 * @class ValidationError
 * @extends {Error}
 */
export class ValidationError extends Error {
    /**
     * Creates an instance of ValidationError
     *
     * @param {string} message the error message
     * @param {Record<string, string>} errors the set of validation property(ies)
     * where the key is the command parameter property name and the value is the LocalData)
     * @memberof ValidationError
     */
    constructor(message: string, public errors: Record<string, LocaleData>) {
        super(message);
        this.name = ValidationError.name;
    }
}
