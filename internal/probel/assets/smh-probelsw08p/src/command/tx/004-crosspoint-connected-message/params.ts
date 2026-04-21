/**
 * CrossPointConnectedMessage command parameters
 * @export
 * @interface CrossPointConnectedMessageCommandParams
 */
export interface CrossPointConnectedMessageCommandParams {
    /**
     * Matrix ID of the matrix to monitor
     * + Range [0 - 255]
     * @type {number}
     * @memberof CrossPointConnectedMessageCommandParams
     */
    matrixId: number;

    /**
     * Level ID of the matrix to monitor
     * + Range [0 - 255]
     * @type {number}
     * @memberof CrossPointConnectedMessageCommandParams
     */
    levelId: number;

    /**
     * Destination ID of the matrix to monitor
     * + Range [0 - 65535]
     * @type {number}
     * @memberof CrossPointConnectedMessageCommandParams
     */
    destinationId: number;

    /**
     * Source ID of the matrix to monitor
     * + Range [0 - 65535]
     * @type {number}
     * @memberof CrossPointConnectedMessageCommandParams
     */

    sourceId: number;

    /**
     * Stastus  (For future use)
     * + forced to 0
     * @type {number}
     * @memberof CrossPointConnectedMessageCommandParams
     */
    statusId: number;
}

/**
 * Utility class providing extra functionalities on the CrossPointTallyMessage command parameters
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
    static toString(params: CrossPointConnectedMessageCommandParams, withStatusId: boolean): string {
        return `Matrix:${params.matrixId}, Level:${params.levelId}, Destination:${params.destinationId}, Source:${
            params.sourceId
        }${withStatusId ? `, Status:${params.statusId}` : ''}`;
    }
}
