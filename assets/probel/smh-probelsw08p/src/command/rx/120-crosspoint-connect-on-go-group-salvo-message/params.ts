import { CrossPointConnectOnGoSalvoGroupMessageCommandItems } from './items';

/**
 * CrossPointConnectOnGoSalvoGroupMessage command parameters
 *
 * @export
 * @interface CrossPointConnectOnGoSalvoGroupMessageCommandParams
 */
export interface CrossPointConnectOnGoSalvoGroupMessageCommandParams {
    /**
     * Salvo number
     * + Destination and source will always overwrite previous data.
     * + Range [0 - 127]
     * @type {CrossPointConnectOnGoSalvoGroupMessageCommandItems[]}
     * @memberof CrossPointConnectOnGoSalvoGroupMessageCommandParams
     */
    salvoGroupMessageCommandItems: CrossPointConnectOnGoSalvoGroupMessageCommandItems[];
}

/**
 * Utility class providing extra functionalities on the CrossPointConnectOnGoGroupSalvo command parameters
 *
 * @export
 * @class CommandParamsUtility
 */
export class CommandParamsUtility {
    /**
     * Gets a textual representation of the command parameters
     *
     * @static
     * @param {CrossPointConnectOnGoSalvoGroupMessageCommandParams} params the command parameters
     * @returns {string} the command parameters textual representation
     * @memberof CommandParamsUtility
     */
    static toString(params: CrossPointConnectOnGoSalvoGroupMessageCommandParams): string {
        // TODO : Add groupMessageCommandItems[]
        return `${this.name}: groupMessageCommandItems[]`;
    }
}
