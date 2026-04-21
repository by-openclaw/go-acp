/**
 * Maintenance Message - describe the functions and following message data.
 *
 * Configure Installed Modules (Byte 1 = 03)
 * + Not implemented
 *
 * @export
 * @enum {number} hard reset, soft reset, clear protects, configure installed modules, database transfer (configure installed modules not supported)
 */
export enum MaintenanceFunction {
    /**
     * Hard Reset (Byte 1 = 00)
     * The Hard reset command will force the controller to completely reset, as
     * though power had just been applied. This would normally be achieved by
     * forcing the watchdog timer to time out thus initiating a hardware reset.
     */
    HARD_RESET = 0x00,

    /**
     * Soft Reset (Byte 1 = 01)
     * The soft reset command will force the controller to do a software reset,
     * e.g. re-initialising after a database download or a main loop restart.
     * This is similar to the hard reset command but * may not re-initialise all the hardware.
     */
    SOFT_RESET = 0x01,

    /* Clear Protects (Byte 1 = 02)
     * This command will clear all crossPoint protects on the controller no matter who has set the protect.
     * This command acts as the ‘MASTER’ protect override.
     * - If the <matrix number> is set to <0FFH> and the <Level number> is not set to 0FFH then that level on all matrices will have their protects cleared.
     * - If the <level number> is set to <0FFH> and the <matrix number> is not set to 0FFH then all levels on that matrix will have their protects cleared.
     * - If both the <matrix number> and the <level number> are set to 0FFH then all levels on all matrices will have their protects cleared.
     */
    CLEAR_PROTECTS = 0x02,

    /**
     * Database Transfer (Byte 01 = 04)
     * This command is used on dual processor controllers and will force the database to be transferred from the ACTIVE controller to the IDLE controller.
     * There are no further message bytes.
     */
    DATABASE_TRANSFERT = 0x04
}

/**
 *  MaintenanceMessage command options
 *
 * @export
 * @interface MaintenanceMessageCommandOptions
 */
export interface MaintenanceMessageCommandOptions {
    /**
     * MaintenanceFunction
     *
     * @type {MaintenanceFunction}
     * @memberof MaintenanceMessageCommandOptions
     */
    maintenanceFunction: MaintenanceFunction;
}

/**
 * Utility class providing some extra functionality on the MaintenanceMessage command options
 *
 * @export
 * @class CommandOptionsUtility
 */
export class CommandOptionsUtility {
    /**
     * Gets a textual representation of the command options
     *
     * @static
     * @param {MaintenanceMessageCommandOptions} options the command options
     * @returns {string} the command options textual representation
     * @memberof CommandOptionsUtility
     */
    static toString(options: MaintenanceMessageCommandOptions): string {
        return `Maintenance Function: ${this.toStringMaintenanceFunction(options.maintenanceFunction)}`;
    }

    /**
     * Gets a textual representation of the MaintenanceFunction
     *
     * @param {MaintenanceFunction} data the MaintenanceFunction
     * @returns {string} the MaintenanceFunction textual representation
     * @memberof CommandOptionsUtility
     */
    static toStringMaintenanceFunction(data: MaintenanceFunction): string {
        return `(${data} - ${MaintenanceFunction[data]})`;
    }

    /**
     * Gets boolean indicating whether the Clear Protects Function is applied
     * + General : false => not applied
     * + Clear Protects : true
     *
     * @static
     * @param {MaintenanceMessageCommandOptions} options the command options
     * @returns {boolean} 'true' if the command is Clear Protects otherwise 'false'
     * @memberof CommandOptionsUtility
     */
    static isClearProtects(options: MaintenanceMessageCommandOptions): boolean {
        return options.maintenanceFunction === MaintenanceFunction.CLEAR_PROTECTS;
    }
}
