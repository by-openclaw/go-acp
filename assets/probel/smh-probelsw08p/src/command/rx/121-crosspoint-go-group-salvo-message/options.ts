/**
 * Status of Go Group Salvo Message Function
 *
 * @export
 * @enum {number}
 */
export enum SalvoMessageFunction {
    /**
     * Set previously received messages
     */
    SET_PREVIOUS_RECEIVED_MESSAGES = 0x00,
    /**
     * Clear previously received messages
     */
    CLEAR_PREVIOUSLY_RECEIVED_MESSAGES = 0x01
}

/**
 * CrossPointGoGroupSalvoMessage command options
 * + Set Set previously received messages
 * + Clear previously received messages
 * @export
 * @interface CrossPointGoGroupSalvoMessageCommandOptions
 */
export interface CrossPointGoGroupSalvoMessageCommandOptions {
    /**
     * SalvoFunction Required
     * - Set previously received messages
     * - Clear previously received messages

     * @type {number}
     * @memberof SalvoCommandOptions
     */
    salvoMessageFunction: SalvoMessageFunction;
}

/**
 * Utility class providing extra functionalities on the CrossPointGoGroupSalvoMessage command options
 *
 * @export
 * @class CommandOptionsUtility
 */
export class CommandOptionsUtility {
    /**
     * Gets a textual representation of the command options
     *
     * @param {CrossPointGoGroupSalvoMessageCommandOptions} options the command options
     * @returns {string} the command options textual representation
     * @memberof CommandOptionsUtility
     */
    static toString(options: CrossPointGoGroupSalvoMessageCommandOptions): string {
        return `lengthOfNames:${this.toStringSalvoMessageFunction(options.salvoMessageFunction)}`;
    }

    /**
     * Gets a textual representation of the SalvoMessageFunction
     *
     * @param {LengthOfNamesRequired} data the SalvoMessageFunction
     * @returns {string} the SalvoMessageFunction textual representation
     * @memberof CommandOptionsUtility
     */
    static toStringSalvoMessageFunction(data: SalvoMessageFunction): string {
        return `(${data} - ${SalvoMessageFunction[data]})`;
    }
}
