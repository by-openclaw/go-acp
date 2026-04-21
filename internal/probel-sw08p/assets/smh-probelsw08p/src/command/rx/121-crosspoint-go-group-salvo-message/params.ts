/**
 * CrossPointGoGroupSalvoMessage command parameters
 *
 * @export
 * @interface CrossPointGoGroupSalvoMessageCommandParams
 */
export interface CrossPointGoGroupSalvoMessageCommandParams {
    /**
     * Salvo number
     * + Destination and source will always overwrite previous data.
     * + Range [0 - 127]
     * @type {number}
     * @memberof CrossPointGoGroupSalvoMessageCommandParams
     */
    salvoId: number;
}

/**
 * Utility class providing extra functionalities on the CrossPointGoGroupSalvoMessage command parameters
 *
 * @export
 * @class CommandParamsUtility
 */
export class CommandParamsUtility {
    /**
     * Gets a textual representation of the command parameters
     *
     * @static
     * @param {CrossPointGoGroupSalvoMessageCommandParams} params the command parameters
     * @returns {string} the command parameters textual representation
     * @memberof CommandParamsUtility
     */
    static toString(params: CrossPointGoGroupSalvoMessageCommandParams): string {
        return `Salvo:${params.salvoId}`;
    }
}
