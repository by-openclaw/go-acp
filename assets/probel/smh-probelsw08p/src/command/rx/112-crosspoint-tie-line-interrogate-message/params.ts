/**
 * CrossPointTieLineInterrogateMessage command parameters
 * @export
 * @interface CrossPointTieLineIneterrogateMessageCommandParams
 */
export interface CrossPointTieLineInterrogateMessageCommandParams {
    /**
     * Destination Matrix Number
     * + Range [0 - 19]
     *
     * @type {number}
     * @memberof CrossPointTieLineInterrogateMessageCommandParams
     */
    matrixId: number;

    /**
     * Destination Association Number
     * + Destination and source will always overwrite previous data.
     * + Range [0 - 65535]
     *
     * @type {number}
     * @memberof CrossPointTieLineInterrogateMessageCommandParams
     */
    destinationId: number;
}

/**
 * Utility class providing extra functionalities on the CrosspPintTieLineInterrogateMessage command parameters
 *
 * @export
 * @class CommandParamsUtility
 */
export class CommandParamsUtility {
    /**
     * Gets a textual representation of the command parameters
     *
     * @static
     * @param {CrossPointTieLineInterrogateMessageCommandParams} params the command parameters
     * @returns {string} the command parameters textual representation
     * @memberof CommandParamsUtility
     */
    static toString(params: CrossPointTieLineInterrogateMessageCommandParams): string {
        return `Matrix:${params.matrixId}, Destination:${params.destinationId}`;
    }
}
