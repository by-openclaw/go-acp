/**
 * CrossPoint Go Done Group Salvo Acknowledge Message Command Input Paramameters
 * @export
 * @interface CrossPointGoDoneGroupSalvoAcknowledgeMessageCommandParams
 */
export interface CrossPointGoDoneGroupSalvoAcknowledgeMessageCommandParams {
    /**
     *
     * Byte 2 - Salvo group number as defined in 3.1.29
     * Salvo number && x07f
     * @type {number}
     * @memberof CrossPointConnectOnGoGroupSalvoAcknowledgeCommandParams
     */
    salvoId: number;
}
/**
 * Utility class providing extra functionalities on the CrosspointGoDoneGroupSalvoAcknowledgeMessage command parameters
 *
 * @export
 * @class CommandParamsUtility
 */
export class CommandParamsUtility {
    /**
     * Gets a textual representation of the command parameters
     *
     * @static
     * @param {CrossPointGoDoneGroupSalvoAcknowledgeMessageCommandParams} params the command parameters
     * @returns {string} the command parameters textual representation
     * @memberof CommandParamsUtility
     */
    static toString(params: CrossPointGoDoneGroupSalvoAcknowledgeMessageCommandParams): string {
        return `Salvo Id:${params.salvoId}`;
    }
}
