import { SmartBuffer } from 'smart-buffer';

import { CommandIdentifiers } from '../../command-contract';
import { CommandBase } from '../../command.base';
import { CommandOptionsUtility, DualControllerStatusResponseMessageCommandOptions } from './options';

/**
 * Implements the Dual Controller Status Response Message command
 *
 * This message is issued by the controller on power-up and in response to a DUAL CONTROLLER STATUS REQUEST (Command byte 08).
 * + activeCardStatus: DualControllerStatusResponseMessage_ActiveCardBit_0
 * + activeStatus: DualControllerStatusResponseMessage_ActiveCardBit_1
 * + idleCardstatus: DualControllerStatusResponseMessage_IdleCardStatus
 *
 * Command issued by Pro-Bel Controller
 *
 * @export
 * @class DualControllerStatusResponseMessageCommand
 * @extends {CommandBase<DualControllerStatusResponseMessageCommandParams>}
 */
export class DualControllerStatusResponseMessageCommand extends CommandBase<
    any,
    DualControllerStatusResponseMessageCommandOptions
> {
    /**
     * Creates an instance of DualControllerStatusResponseMessageCommand
     *
     * @param {DualControllerStatusResponseMessageCommandOptions} options the command parameters
     * @memberof DualControllerStatusResponseMessageCommand
     */
    constructor(options: DualControllerStatusResponseMessageCommandOptions) {
        super(CommandIdentifiers.TX.GENERAL.DUAL_CONTROLLER_STATUS_RESPONSE_MESSAGE, {}, options);
    }

    /**
     * Gets a textual representation of the command including params (mainly used for logging purpose)
     *
     * @returns {string} the textual command representation
     * @memberof DualControllerStatusResponseMessageCommand
     */
    toLogDescription(): string {
        const descriptionFor = (type: string): string =>
            `${type} - ${this.name}: ${CommandOptionsUtility.toString(this.options)}`;
        return descriptionFor('General');
    }

    /**
     * Builds the command
     *
     * @protected
     * @returns {Buffer} the command message
     * @memberof DualControllerStatusResponseMessageCommand
     */
    protected buildData(): Buffer {
        return this.buildDataNormal();
    }

    /**
     * Build the general command
     * Returns Probel SW-P-08 - General Dual Controller Status Response Message CMD_009_0X09
     *
     * + This message is issued by the controller on power-up and in response to a DUAL CONTROLLER STATUS REQUEST (Command byte 08).
     *
     * | Message | Command Byte | 009 - 0x09                                                                                                                         |
     * |---------|--------------|------------------------------------------------------------------------------------------------------------------------------------|
     * | Byte    | Field Format | Notes                                                                                                                              |
     * | Byte 1  |Bit[0]        | Active Card Status                                                                                                                 |
     * |         |              | 0 = MASTER is active                                                                                                               |
     * |         |              | 1 = SLAVE is active                                                                                                                |
     * | Byte 1  |Bit[1]        | Active status                                                                                                                      |
     * |         |              | 0 = Inactive                                                                                                                       |
     * |         |              | 1 = Active                                                                                                                         |
     * | Byte 2  | Idle Card    | Idle Card Status                                                                                                                   |
     * |         | 0            | Idle controller is OK                                                                                                              |
     * |         | 1            | Idle controller is missing/faulty                                                                                                  |
     *
     * @private
     * @returns {Buffer} the command message
     * @memberof DualControllerStatusResponseMessageCommand
     */
    private buildDataNormal(): Buffer {
        return new SmartBuffer({ size: 3 })
            .writeUInt8(this.id)
            .writeUInt8(this.options.activeCardStatus | (this.options.activeStatus << 1))
            .writeUInt8(this.options.idleCardstatus)
            .toBuffer();
    }
}
