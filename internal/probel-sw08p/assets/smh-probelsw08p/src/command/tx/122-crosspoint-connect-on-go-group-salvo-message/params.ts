/**
 * Crosspoint Go Done Group Salvo Acknowledge Message Command Input Paramameters
 * @export
 * @interface CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommandParams
 */
export interface CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommandParams {
    /**
     * Matrix ID of the matrix to monitor
     * + Range [0 - 255]
     * @type {number}
     * @memberof CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommandParams
     */
    matrixId: number;

    /**
     * Level ID of the matrix to monitor
     * + Range [0 - 255]
     * @type {number}
     * @memberof CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommandParams
     */
    levelId: number;

    /**
     * Destination ID of the matrix to monitor
     * + Range [0 - 65535]
     * @type {number}
     * @memberof CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommandParams
     */
    destinationId: number;

    /**
     * Source ID of the matrix to monitor
     * + Range [0 - 65535]
     * @type {number}
     * @memberof CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommandParams
     */
    sourceId: number;

    /**
     * Salvo number
     * + Destination and source will always overwrite previous data.
     * + Range [0 - 127]
     * @type {number}
     * @memberof CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommandParams
     */
    salvoId: number;
}

/**
 * Utility class providing extra functionalities on the CrossPointConnectOnGoGroupSalvoAcknowledgeMessage command parameters
 *
 * @export
 * @class CommandParamsUtility
 */
export class CommandParamsUtility {
    /**
     * Gets a textual representation of the command parameters
     *
     * @static
     * @param {CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommandParams} the command parameters
     * @returns {string} the command parameters textual representation
     * @memberof CommandParamsUtility
     */
    static toString(params: CrossPointConnectOnGoGroupSalvoAcknowledgeMessageCommandParams): string {
        return `Matrix:${params.matrixId}, Level:${params.levelId}, Destination:${params.destinationId}, Source:${params.sourceId}, SalvoId:${params.salvoId}`;
    }
}
