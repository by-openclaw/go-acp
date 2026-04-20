import { SmartBuffer } from 'smart-buffer';

import { CommandIdentifiers } from '../../command-contract';
import { CommandBase } from '../../command.base';

/**
 * Implements the Dual Controller Status Request Message command
 *
 * This message is issued by the remote device to the active controller to ascertain the status of both controllers in a dual system.
 * The controller will respond with a DUAL CONTROLLER STATUS RESPONSE message (Command Byte 09).
 *
 * Command issued by Pro-Bel Controller
 *
 * @export
 * @class DualControllerStatusRequestMessageCommand
 * @extends {CommandBase<DualControllerStatusRequestMessageCommandParams>}
 */
export class DualControllerStatusRequestMessageCommand extends CommandBase<any, any> {
    /**
     * Creates an instance of DualControllerStatusRequestMessageCommand
     *
     * @memberof DualControllerStatusRequestMessageCommand
     */
    constructor() {
        super(CommandIdentifiers.RX.GENERAL.DUAL_CONTROLLER_STATUS_REQUEST_MESSAGE, {});
    }

    /**
     * Gets a textual representation of the command (mainly used for logging purpose)
     *
     * @returns {string} the command textual representation
     * @memberof DualControllerStatusRequestMessageCommand
     */
    toLogDescription(): string {
        return `General - ${this.name}`;
    }

    /**
     * Builds the command
     *
     * @protected
     * @returns {Buffer} the command message
     * @memberof DualControllerStatusRequestMessageCommand
     */
    protected buildData(): Buffer {
        return this.buildDataNormal();
    }

    /**
     * Build the general command
     * Returns Probel SW-P-08 - General Dual Controller Status Request Messagee CMD_008-0x08
     *
     * + This message is issued by the remote device and allows various maintenance functions to be performed on the controller, i.e. hard reset, soft reset, clear protects, configure installed modules, database transfer.
     * + The number of bytes following the command byte is dependent on the operation being performed.
     *
     * | Message | Command Byte | 008 - 0x08                                                                                                                         |
     * |---------|--------------|------------------------------------------------------------------------------------------------------------------------------------|
     * | Byte    | Field Format | Notes                                                                                                                              |
     * | Byte 1  |              | No Message Bytes                                                                                                                   |
     *
     * @protected
     * @returns {Buffer} the command message
     * @memberof DualControllerStatusRequestMessage
     */
    protected buildDataNormal(): Buffer {
        return new SmartBuffer({ size: 1 }).writeUInt8(this.id).toBuffer();
    }
}
