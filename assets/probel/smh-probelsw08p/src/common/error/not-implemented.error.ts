/**
 * Error raised when a method, setter, getter, ... is not yet implemented
 *
 * @export
 * @class NotImplementedError
 * @extends {Error}
 */
export class NotImplementedError extends Error {
    /**
 *Creates an instance of NotImplementedError

 * @param {Maybe<string>} [message] the optional error message
 * @memberof NotImplementedError
 */
    constructor(message?: string) {
        super(message ?? '');
        this.name = NotImplementedError.name;
    }
}
