/**
 * CrossPointInterrogateMessage command parameters
 *
 * @export
 * @interface CrossPointInterrogateMessageCommandParams
 */
export interface CrossPointInterrogateMessageCommandParams {
    /**
     * Matrix ID of the matrix to monitor
     * + Range [0 - 255]
     *
     * @type {number}
     * @memberof CrossPointInterrogateMessageCommandParams
     */
    matrixId: number;

    /**
     * Level ID of the matrix to monitor
     * + Range [0 - 255]
     *
     * @type {number}
     * @memberof CrossPointInterrogateMessageCommandParams
     */
    levelId: number;

    /**
     * Destination ID of the matrix to monitor
     * + Range [0 - 65535]
     *
     * @type {number}
     * @memberof CrossPointInterrogateMessageCommandParams
     */
    destinationId: number;
}

/**
 * Utility class providing extra functionalities on the CrossPointInterrogateMessage command parameters
 *
 * @export
 * @class CommandParamsUtility
 */
export class CommandParamsUtility {
    /**
     * Gets a textual representation of the command parameters
     *
     * @static
     * @param {CrossPointInterrogateMessageCommandParams} params the command parameters
     * @returns {string} the command parameters textual representation
     * @memberof CommandParamsUtility
     */
    static toString(params: CrossPointInterrogateMessageCommandParams): string {
        return `Matrix:${params.matrixId}, Level:${params.levelId}, Destination:${params.destinationId}`;
    }
}
