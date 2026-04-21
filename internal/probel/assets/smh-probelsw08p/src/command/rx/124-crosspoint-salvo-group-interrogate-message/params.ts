/**
 * CrossPointSalvoGroupInterrogateMessage command parameters
 *
 * @export
 * @interface CrossPointSalvoGroupInterrogateMessageCommandParams
 */
export interface CrossPointSalvoGroupInterrogateMessageCommandParams {
    /**
     * Connect Index
     * + this specifies the index into the SALVO GROUP specified in byte1.
     * This command is called recursively from connect index 0, until no crosspoint data in the specified group is left.
     * + Range [0 - 65535]
     *
     * @type {number}
     * @memberof CrossPointSalvoGroupInterrogateMessageCommandParams
     */
    connectIndexId: number;

    /**
     * Salvo Group number
     * + Destination and source will always overwrite previous data.
     * + Range [0 - 127]
     *
     * @type {number}
     * @memberof CrossPointSalvoGroupInterrogateMessageCommandParams
     */
    salvoId: number;
}

/**
 * Utility class providing extra functionalities on the CrossPointSalvoGroupInterrogateMessage command parameters
 *
 * @export
 * @class CommandParamsUtility
 */
export class CommandParamsUtility {
    /**
     * Gets a textual representation of the command parameters
     *
     * @static
     * @param {CrossPointSalvoGroupInterrogateMessageCommandParams} params the command parameters
     * @returns {string} the command parameters textual representation
     * @memberof CommandParamsUtility
     */
    static toString(params: CrossPointSalvoGroupInterrogateMessageCommandParams): string {
        return `Connect Index:${params.connectIndexId}, Salvo:${params.salvoId}`;
    }
}
