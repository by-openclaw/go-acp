/**
 * Probel SW-P-08 - General Dual Controller Status Response Message CMD_009_0X09
 * Gets the index of the Active Card Status (Byte 1  Bit[0] = 00)  describe the functions and following message data.

* (Byte 1  Bit[0]) describe the functions and following message data.
 * + 0 = MASTER is active
 * + 1 = SLAVE is active
 *
 * @export
 * @enum {number}
 */
export enum ActiveCardBit_0 {
    /**
     * STATUS REQUEST
     */
    MASTER_IS_ACTIVE = 0x00,
    /**
     * STATUS REQUEST
     */
    SLAVE_IS_ACTIVE = 0x01
}

/**
 * Probel SW-P-08 - General Dual Controller Status Response Message CMD_009_0X09
 * Gets the index of the Active Status Response Message
 *
 * Active status (Byte 1  Bit[1])  describe the functions and following message data.
 * + 0 = Inactive
 * + 1 = Active
 *
 * @export
 * @enum {number}
 */
export enum ActiveCardBit_1 {
    /**
     * STATUS REQUEST
     */
    INACTIVE = 0x00,
    /**
     * STATUS REQUEST
     */
    ACTIVE = 0x01
}

/**
 * Probel SW-P-08 - General Dual Controller Status Response Message CMD_009_0X09
 * Gets the index of the Idle Card Status Response Message
 *
 * Idle Card Status (Byte 2)  describe the functions and following message data.
 * + 0 = Idle controller is OK
 * + 1 = Idle controller is missing/faulty
 *
 * @export
 * @enum {number}
 */
export enum IdleCardStatus {
    /**
     * STATUS REQUEST
     */
    IDLE_CONTROLLER_IS_OK = 0x00,
    /**
     * STATUS REQUEST
     */
    IDEL_CONTROLLER_IS_MISSING_FAULTY = 0x01
}

/**
 * DualControllerStatusResponseMessage command options
 *
 * @export
 * @interface DualControllerStatusResponseMessageCommandOptions
 */
export interface DualControllerStatusResponseMessageCommandOptions {
    /**
     * Returns the Dual Controller Status Response Message
     *
     * Active Card Status (Byte 1  Bit[0] = 00)
     * + 0 = MASTER is active
     * + 1 = SLAVE is active
     *
     * @type {ActiveCardBit_0}
     * @memberof DualControllerStatusResponseMessageCommandOptions
     */
    activeCardStatus: ActiveCardBit_0;

    /**
     * Returns the Dual Controller Status command options
     *
     * Active status (Byte 1  Bit[1] = 01)
     * + 0 = Inactive
     * + 1 = Active
     * @type {ActiveCardBit_1}
     * @memberof DualControllerStatusResponseMessageCommandOptions
     */
    activeStatus: ActiveCardBit_1;

    /**
     * Returns the Dual Controller Status command options
     *
     * Idle Card Status (Byte 2 = 02)
     * + 0 = Idle controller is OK
     * + 1 = Idle controller is missing/faulty
     *
     * @type {IdleCardStatus}
     * @memberof DualControllerStatusResponseMessageCommandOptions
     */
    idleCardstatus: IdleCardStatus;
}

/**
 * Utility class providing some extra functionality on the DualControllerStatusResponseMessage command options
 *
 * @export
 * @class CommandOptionsUtility
 */
export class CommandOptionsUtility {
    /**
     * Gets a textual representation of the command parameters
     *
     * @param {DualControllerStatusResponseMessageCommandOptions} options the command options
     * @returns {string} the command options textual representation
     */
    static toString(options: DualControllerStatusResponseMessageCommandOptions): string {
        return `active Card Status: ${this.toStringActiveCardBit_0(
            options.activeCardStatus
        )}, active Status: ${this.toStringActiveCardBit_1(
            options.activeStatus
        )}, idle Card Status: ${this.toStringIdleCardStatus(options.idleCardstatus)}`;
    }

    /**
     * Gets a textual representation of the ActiveCardBit_0
     *
     *
     * @param {MaintenanceFunction} data the ActiveCardBit_0
     * @returns {string} the textual representation
     * @memberof CommandOptionsUtility
     */
    static toStringActiveCardBit_0(data: ActiveCardBit_0): string {
        return `(${data} - ${ActiveCardBit_1[data]})`;
    }

    /**
     * Gets a textual representation of the ActiveCardBit_1
     *
     *
     * @param {MaintenanceFunction} data the MaintenanceFunction
     * @returns {string} the textual representation
     * @memberof CommandOptionsUtility
     */
    static toStringActiveCardBit_1(data: ActiveCardBit_1): string {
        return `(${data} - ${ActiveCardBit_1[data]})`;
    }

    /**
     * Gets a textual representation of the MaintenanceFunction
     *
     *
     * @param {MaintenanceFunction} data the IdleCardStatus
     * @returns {string} the textual representation
     * @memberof CommandOptionsUtility
     */
    static toStringIdleCardStatus(data: IdleCardStatus): string {
        return `(${data} - ${IdleCardStatus[data]})`;
    }
}
