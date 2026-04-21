/**
 * Crosspoint Go Done Group Salvo Acknowledge Message command Input option
 *
 * @export
 * @enum {number}
 */
export enum SalvoCrossPointStatus {
    /**
     * CrossPoints set
     */
    CROSSPOINT_SET = 0x00,

    /**
     * Stored crosspoints cleared
     */
    STORED_CROSSPOINTS_CLEARED = 0x01,

    /**
     * No crosspoints to set / clear.
     */
    CROSSPOINT_TO_SET_OR_CLEAR = 0x02
}

/**
 * CrossPoint Go Done Group Salvo Acknowledge Message Command Options
 *
 * @export
 * @interface CrossPointGoDoneGroupSalvoAcknowledgeMessageCommandOptions
 */
export interface CrossPointGoDoneGroupSalvoAcknowledgeMessageCommandOptions {
    /**
     * Salvo crosspoint status
     *
     * @type {number}
     * @memberof CrossPointGoDoneGroupSalvoAcknowledgeMessageCommandOptions
     */
    salvoCrossPointStatus: SalvoCrossPointStatus;
}

/**
 * Utility class providing some extra functionality on the CrosspointGoDoneGroupSalvoAcknowledgeMessage command options
 *
 * @export
 * @class CommandOptionsUtility
 */
export class CommandOptionsUtility {
    /**
     * Gets a textual representation of the command options
     *
     * @param {CrossPointGoDoneGroupSalvoAcknowledgeMessageCommandOptions} options the command options
     * @returns {string} the command options textual representation
     * @memberof CommandOptionsUtility
     */
    static toString(options: CrossPointGoDoneGroupSalvoAcknowledgeMessageCommandOptions): string {
        return `salvoCrossPointStatus:${this.toStringSalvoCrossPointStatus(options.salvoCrossPointStatus)}`;
    }

    /**
     * Gets a textual representation of the CrosspointGoDoneGroupSalvoAcknowledgeMessage
     *
     * @static
     * @param {ProtectDetails} data the SalvoCrossPointStatus
     * @returns {string} the SalvoCrossPointStatus textual representation
     * @memberof CommandOptionsUtility
     */
    static toStringSalvoCrossPointStatus(data: SalvoCrossPointStatus): string {
        return `( ${data} - ${SalvoCrossPointStatus[data]} )`;
    }
}
